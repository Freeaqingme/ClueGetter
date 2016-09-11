// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/glenn-brown/golang-pkg-pcre/src/pkg/pcre"
	redis "gopkg.in/redis.v3"
)

type RedisClientBase interface {
	Del(keys ...string) *redis.IntCmd
	Exists(key string) *redis.BoolCmd
	Expire(key string, expiration time.Duration) *redis.BoolCmd
	Get(key string) *redis.StringCmd
	HSet(key, field, value string) *redis.BoolCmd
	HGetAllMap(key string) *redis.StringStringMapCmd
	Incr(key string) *redis.IntCmd
	LPush(key string, values ...string) *redis.IntCmd
	LPushX(key, value interface{}) *redis.IntCmd
	LRange(key string, start, stop int64) *redis.StringSliceCmd
	LSet(key string, index int64, value interface{}) *redis.StatusCmd
	Ping() *redis.StatusCmd
	RPop(key string) *redis.StringCmd
	SAdd(key string, members ...string) *redis.IntCmd
	SMembers(key string) *redis.StringSliceCmd
	Set(key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	SetNX(key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	ZAdd(key string, members ...redis.Z) *redis.IntCmd
	ZCount(key, min, max string) *redis.IntCmd
	ZRemRangeByScore(key, min, max string) *redis.IntCmd

	Eval(string, []string, []string) *redis.Cmd
	EvalSha(sha1 string, keys []string, args []string) *redis.Cmd
	ScriptExists(scripts ...string) *redis.BoolSliceCmd
	ScriptLoad(script string) *redis.StringCmd
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

var (
	redisClient    RedisClient
	RedisLPushChan chan *RedisKeyValue
	redisDumpKeys  = make([]*pcre.Regexp, 0)
)

func redisStart() {
	if !Config.Redis.Enabled {
		return
	}

	if len(Config.Redis.Host) == 0 {
		Log.Fatalf("No Redis.Host specified")
	}

	if Config.Redis.Dump_Dir != "" {
		if len(Config.Redis.Dump_Key) == 0 {
			Config.Redis.Dump_Key = []string{"^cluegetter!"}
		}
		for _, regexStr := range Config.Redis.Dump_Key {
			regex, err := pcre.Compile(regexStr, 0)
			if err != nil {
				Log.Fatalf("Could not compile redis key regex: /%s/. Error: %s", regexStr, err.String())
			}
			redisDumpKeys = append(redisDumpKeys, &regex)
		}
	}

	RedisLPushChan = make(chan *RedisKeyValue, 255)
	redisClient = redisNewClient()
	cg.redis = redisClient

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
	Log.Infof("Redis module started successfully")

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
		Log.Fatalf("Unknown redis connection method specified")
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
	Log.Debugf("Added 1 item to Redis Queue %s. New size: %d", cmd.key, res.Val())
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
					Log.Errorf("Error while polling from Redis: %s", err.Error())
					time.Sleep(5 * time.Second)
					break
				}
				for {
					rdbmsErr := Rdbms.Ping()
					if rdbmsErr == nil {
						break
					}
					Log.Errorf("Mysql seems down: %s", rdbmsErr.Error())
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
	Log.Debugf("Running redisUpdateRunningList()")

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
			Log.Errorf("Could not parse json service string: %s", err.Error())
			continue
		}
		out = append(out, value)
	}

	return out
}

func RedisPublish(key string, msg []byte) error {
	var logMsg string
	if len(logMsg) > 128 {
		logMsg = string(logMsg[:128]) + "..."
	} else {
		logMsg = string(logMsg)
	}

	redisDumpPublish("out", key, msg)
	Log.Infof("Publising on Redis channel '%s': %s", key, logMsg)
	return redisClient.Publish(key, string(msg)).Err()
}

func redisRpc() {
	pubsub, err := redisClient.PSubscribe("cluegetter!*")
	if err != nil {
		Log.Fatalf("Could not connect to Redis or subscribe to the RPC channels: ", err.Error())
	}
	defer pubsub.Close()

	listeners := make(map[string][]chan string, 0)
	for _, module := range cg.Modules() {
		for pattern, channel := range module.Rpc() {
			if listeners[pattern] == nil {
				listeners[pattern] = make([]chan string, 0)
			}
			listeners[pattern] = append(listeners[pattern], channel)
		}
	}

	for {
		msg, err := pubsub.ReceiveMessage()
		if err != nil {
			Log.Errorf("Error from redis/pubsub.ReceiveMessage(): %s", err.Error())
			time.Sleep(1 * time.Second)
			continue
		}

		logMsg := msg.Payload
		if len(logMsg) > 128 {
			logMsg = logMsg[:128] + "..."
		}
		logMsg = strings.Replace(logMsg, "\n", "\\n", -1)

		elements := strings.SplitN(msg.Channel, "!", 3)
		if len(elements) < 3 || elements[0] != "cluegetter" {
			Log.Noticef("Received invalid RPC channel <%s>%s", msg.Channel, logMsg)
			continue
		}

		if msgInstance, err := strconv.Atoi(elements[1]); err == nil && len(elements[1]) > 0 {
			if msgInstance != int(instance) {
				Log.Debugf("Received RPC message for other instance (%d). Ignoring: <%s>%s", instance, msg.Channel, logMsg)
				continue
			}
		} else if len(elements[1]) > 0 && elements[1] != hostname {
			Log.Debugf("Received RPC message for other service (%s). Ignoring: <%s>%s", elements[1], msg.Channel, logMsg)
			continue
		}

		if listeners[elements[2]] == nil {
			Log.Debugf("Received RPC message but no such pattern was registered, ignoring: <%s>%s", msg.Channel, logMsg)
			continue
		}

		Log.Infof("Received RPC Message: <%s>%s", msg.Channel, logMsg)
		redisDumpPublish("in", msg.Channel, []byte(msg.Payload))
		for _, channel := range listeners[elements[2]] {
			go func(payload string) {
				channel <- payload
			}(msg.Payload)
		}
	}
}

func redisDumpPublish(direction, key string, msg []byte) {
	if Config.Redis.Dump_Dir == "" {
		return
	}

	dump := false
	for _, regexp := range redisDumpKeys {
		if len(regexp.FindIndex([]byte(key), 0)) != 0 {
			dump = true
			break
		}
	}
	if !dump {
		return
	}

	filename := fmt.Sprintf("cluegetter-redisPublish-%s-%s-", key, direction)
	f, err := ioutil.TempFile(Config.Redis.Dump_Dir, filename)
	if err != nil {
		Log.Errorf("Could not open file for dump file: %s", err.Error())
		return
	}

	defer f.Close()
	count, err := f.Write(msg)
	if err != nil {
		Log.Errorf("Wrote %d bytes to '%s', then got error: %s", count, f.Name(), err.Error())
		return
	}

	Log.Debugf("Wrote %d bytes to '%s'", count, f.Name())
}
