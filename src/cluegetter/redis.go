// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"encoding/json"
	"fmt"
	redis "gopkg.in/redis.v3"
	"math"
	"strconv"
	"strings"
	"time"
)

type RedisClientBase interface {
	Del(keys ...string) *redis.IntCmd
	Exists(key string) *redis.BoolCmd
	Expire(key string, expiration time.Duration) *redis.BoolCmd
	Get(key string) *redis.StringCmd
	HSet(key, field, value string) *redis.BoolCmd
	HGetAllMap(key string) *redis.StringStringMapCmd
	LPush(key string, values ...string) *redis.IntCmd
	LPushX(key, value interface{}) *redis.IntCmd
	LRange(key string, start, stop int64) *redis.StringSliceCmd
	LSet(key string, index int64, value interface{}) *redis.StatusCmd
	Ping() *redis.StatusCmd
	RPop(key string) *redis.StringCmd
	SAdd(key string, members ...string) *redis.IntCmd
	SMembers(key string) *redis.StringSliceCmd
	Set(key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	ZAdd(key string, members ...redis.Z) *redis.IntCmd
	ZCount(key, min, max string) *redis.IntCmd
	ZRemRangeByScore(key, min, max string) *redis.IntCmd
}

type RedisClient interface {
	RedisClientBase

	PSubscribe(patterns ...string) (*redis.PubSub, error)
	Publish(channel, message string) *redis.IntCmd
}

type RedisClientMulti interface {
	RedisClientBase
	Exec(f func() error) ([]redis.Cmder, error)
}

type RedisKeyValue struct {
	key   string
	value []byte
}

var redisClient RedisClient
var RedisLPushChan chan *RedisKeyValue

func persistStart() {
	if !Config.Redis.Enabled {
		return
	}

	if len(Config.Redis.Host) == 0 {
		Log.Fatal("No Redis.Host specified")
	}

	RedisLPushChan = make(chan *RedisKeyValue, 255)
	redisClient = redisNewClient()

	go redisChannelListener()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for {
			select {
			case <-ticker.C:
				redisUpdateRunningList()
			}
		}
	}()
	go redisUpdateRunningList()
	go redisRpc()
	Log.Info("Redis module started successfully")

}

func redisNewClient() RedisClient {
	var client RedisClient

	switch Config.Redis.Method {
	case "standalone":
		client = redis.NewClient(&redis.Options{
			Addr:     Config.Redis.Host[0],
			Password: "",
			DB:       0,
		})
	case "sentinel":
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    "master",
			SentinelAddrs: Config.Redis.Host,
		})
	default:
		Log.Fatal("Unknown redis connection method specified")
	}

	return client
}

// Because transactions can be blocking the rest of the
// connection we set up a separate client for the transaction
func redisNewTransaction(keys ...string) (RedisClientMulti, error) {
	client := redisNewClient().(*redis.Client)
	return client.Watch(keys...)
}

func redisChannelListener() {
	for {
		select {
		case cmd := <-RedisLPushChan:
			go redisLPush(cmd)
		}
	}
}

func redisLPush(cmd *RedisKeyValue) {
	res := redisClient.LPush(cmd.key, string(cmd.value))
	Log.Debug("Added 1 item to Redis Queue %s. New size: %d", cmd.key, res.Val())
}

func redisListSubscribe(list string, input chan []byte, output chan []byte) {
	if !Config.Redis.Enabled {
		go redisListSubscribeBypass(input, output)
		return
	}

	go redisListSubscriptionPoller(list, output)
	go redisListSubscriptionQueueHandler(list, input)
}

func redisListSubscriptionQueueHandler(list string, input chan []byte) {
	for {
		data := <-input
		go redisLPush(&RedisKeyValue{list, data})
	}
}

func redisListSubscriptionPoller(list string, output chan []byte) {
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-ticker.C:
			for {
				res, err := redisClient.RPop(list).Bytes()
				if err == redis.Nil {
					break
				}
				if err != nil {
					Log.Error("Error while polling from Redis: %s", err.Error())
					time.Sleep(5 * time.Second)
					break
				}
				for {
					rdbmsErr := Rdbms.Ping()
					if rdbmsErr == nil {
						break
					}
					Log.Error("Mysql seems down: %s", rdbmsErr.Error())
					time.Sleep(2500 * time.Millisecond)
				}

				output <- res
			}
		}
	}
}

func redisListSubscribeBypass(input chan []byte, output chan []byte) {
	for {
		data := <-input
		output <- data
	}
}

type service struct {
	Hostname string
	Instance uint
}

func redisUpdateRunningList() {
	Log.Debug("Running redisUpdateRunningList()")

	now := time.Now().Unix()
	start := int(float64(now) - math.Mod(float64(now), 60.0))

	values := &service{
		Hostname: hostname,
		Instance: instance,
	}
	value, _ := json.Marshal(values)
	valueStr := string(value)

	for i := -1; i <= 1; i++ {
		key := fmt.Sprintf("cluegetter-%d-service-%d", instance, start+(i*60))
		redisClient.HSet(key, hostname, valueStr)
		redisClient.Expire(key, time.Duration(120)*time.Second)

		key = fmt.Sprintf("cluegetter-service-%d", start+(i*60))
		redisClient.SAdd(key, valueStr)
		redisClient.Expire(key, time.Duration(120)*time.Second)
	}
}

func redisGetServices() []*service {
	now := time.Now().Unix()
	start := int(float64(now) - math.Mod(float64(now), 60.0))
	key := fmt.Sprintf("cluegetter-service-%d", start)

	out := make([]*service, 0)
	for _, jsonStr := range redisClient.SMembers(key).Val() {
		value := &service{}
		err := json.Unmarshal([]byte(jsonStr), &value)
		if err != nil {
			Log.Error("Could not parse json service string: %s", err.Error())
			continue
		}
		out = append(out, value)
	}

	return out
}

func redisRpc() {
	pubsub, err := redisClient.PSubscribe("cluegetter!*")
	if err != nil {
		panic(err)
	}
	defer pubsub.Close()

	listeners := make(map[string][]chan string, 0)
	for _, module := range modules {
		if module.rpc == nil {
			continue
		}

		for pattern, channel := range module.rpc {
			if listeners[pattern] == nil {
				listeners[pattern] = make([]chan string, 0)
			}
			listeners[pattern] = append(listeners[pattern], channel)
		}
	}

	for {
		msg, err := pubsub.ReceiveMessage()
		if err != nil {
			Log.Error("Error from redis/pubsub.ReceiveMessage(): %s", err.Error())
			time.Sleep(1 * time.Second)
			continue
		}

		logMsg := msg.Payload
		if len(logMsg) > 128 {
			logMsg = logMsg[:128] + "..."
		}

		elements := strings.SplitN(msg.Channel, "!", 3)
		if len(elements) < 3 || elements[0] != "cluegetter" {
			Log.Notice("Received invalid RPC channel <%s>%s", msg.Channel, logMsg)
			continue
		}

		if msgInstance, err := strconv.Atoi(elements[1]); err == nil && len(elements[1]) > 0 {
			if msgInstance != int(instance) {
				Log.Debug("Received RPC message for other instance (%d). Ignoring: <%s>%s", instance, msg.Channel, logMsg)
				continue
			}
		} else if len(elements[1]) > 0 && elements[1] != hostname {
			Log.Debug("Received RPC message for other service (%s). Ignoring: <%s>%s", elements[1], msg.Channel, logMsg)
			continue
		}

		if listeners[elements[2]] == nil {
			Log.Debug("Received RPC message but no such pattern was registered, ignoring: <%s>%s", msg.Channel, logMsg)
			continue
		}

		Log.Info("Received RPC Message: <%s>%s", msg.Channel, logMsg)
		for _, channel := range listeners[elements[2]] {
			go func(payload string) {
				channel <- payload
			}(msg.Payload)
		}
	}
}
