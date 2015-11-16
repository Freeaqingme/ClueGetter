// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	redis "gopkg.in/redis.v3"
	"time"
)

type RedisClient interface {
	Del(keys ...string) *redis.IntCmd
	Exists(key string) *redis.BoolCmd
	Expire(key string, expiration time.Duration) *redis.BoolCmd
	Get(key string) *redis.StringCmd
	LPush(key string, values ...string) *redis.IntCmd
	LPushX(key, value string) *redis.IntCmd
	LRange(key string, start, stop int64) *redis.StringSliceCmd
	LSet(key string, index int64, value string) *redis.StatusCmd
	Ping() *redis.StatusCmd
	RPop(key string) *redis.StringCmd
	Set(key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	ZAdd(key string, members ...redis.Z) *redis.IntCmd
	ZCount(key, min, max string) *redis.IntCmd
	ZRemRangeByScore(key, min, max string) *redis.IntCmd
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

	RedisLPushChan = make(chan *RedisKeyValue, 255)
	var client RedisClient

	if len(Config.Redis.Host) == 0 {
		Log.Fatal("No Redis.Host specified")
	}

	switch Config.Redis.Method {
	case "standalone":
		client = redis.NewClient(&redis.Options{
			Addr:     Config.Redis.Host[0],
			Password: "",
			DB:       0,
		})
	case "cluster":
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs: Config.Redis.Host,
		})
	case "sentinel":
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    "master",
			SentinelAddrs: Config.Redis.Host,
		})
	default:
		Log.Fatal("Unknown redis connection method specified")

	}

	redisClient = client
	go redisChannelListener()
	Log.Info("Redis module started successfully")
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
