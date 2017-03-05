package core

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	messagePersistQueue = make(chan []byte, 100)
)

func messagePersistStart() {
	messagePersistQueue = make(chan []byte)

	ticker := time.NewTicker(time.Second * 30)
	go func() {
		for range ticker.C {
			MessagePersistCache.prune()
		}
	}()
}

func MessagePersistUnmarshalProto(protoBuf []byte) (*Proto_Message, error) {
	msg := &Proto_Message{}
	err := msg.Unmarshal(protoBuf)
	if err != nil {
		return nil, errors.New("Error unmarshalling message: " + err.Error())
	}

	return msg, nil
}

func messagePersistInCache(queueId string, msgId string, msg []byte) {
	if ok, err := MessagePersistCache.Set(queueId, msgId, msg); !ok {
		Log.Noticef("Could not add message %s to message cache: %s",
			queueId, err.Error())
	}
}

///////////////////
// Message Cache //
///////////////////

var MessagePersistCache = messageCacheNew(5*1024*1024, 512*1024*1024)

type messageCache struct {
	sync.RWMutex
	cache        map[string][]byte
	age          map[string]int64
	msgIdIdx     map[string]string
	msgIdRevIdx  map[string]string
	size         uint64
	maxSize      uint64
	maxEntrySize uint64
}

func messageCacheNew(maxEntrySize uint64, maxSize uint64) *messageCache {
	return &messageCache{
		cache:        make(map[string][]byte),
		age:          make(map[string]int64),
		msgIdIdx:     make(map[string]string),
		msgIdRevIdx:  make(map[string]string),
		size:         0,
		maxSize:      maxSize,
		maxEntrySize: maxEntrySize,
	}
}

func (c *messageCache) getByQueueId(queueId string) []byte {
	c.RLock()
	defer c.RUnlock()

	return c.cache[queueId]
}

func (c *messageCache) GetByMessageId(msgId string) []byte {
	c.RLock()
	queueId := c.msgIdIdx[msgId]
	protoBuf := c.cache[queueId]
	c.RUnlock()

	return protoBuf
}

func (c *messageCache) Set(queueId, msgId string, msg []byte) (bool, error) {
	size := uint64(len(msg))
	if size > c.maxEntrySize {
		return false, errors.New(fmt.Sprintf(
			"Could not cache item '%s' (size %d). Item is bigger than max entry size %d",
			queueId, size, c.maxEntrySize,
		))
	}

	c.Lock()
	defer c.Unlock()

	if c.size+size > c.maxSize {
		return false, errors.New(fmt.Sprintf(
			"Could not cache item '%s' (size %d). Total cache size would be exceeded.",
			queueId, size,
		))
	}

	if _, exists := c.cache[queueId]; exists {
		c._del(queueId)
	}

	c.msgIdIdx[msgId] = queueId
	c.msgIdRevIdx[queueId] = msgId

	c.cache[queueId] = msg
	c.age[queueId] = time.Now().Unix()
	atomic.AddUint64(&c.size, size)

	return true, nil
}

func (c *messageCache) _del(id string) {
	item, exists := c.cache[id]
	if !exists {
		return
	}

	size := len(item)
	delete(c.cache, id)
	delete(c.age, id)
	atomic.AddUint64(&c.size, ^uint64(size-1))

	delete(c.msgIdIdx, c.msgIdRevIdx[id])
	delete(c.msgIdRevIdx, id)
}

func (c *messageCache) prune() {
	if c.size < c.maxSize/2 {
		Log.Debugf("Skipping pruning of messageCache it's below 50%% (%d/%d) capacity.", c.size, c.maxSize)
		return
	}

	t0 := time.Now()
	cutoff := t0.Unix() - 300
	prune := make([]string, 0)
	c.RLock()
	for key, age := range c.age {
		if age < cutoff {
			prune = append(prune, key)
		}
	}
	c.RUnlock()
	Log.Debugf("Scanned for prunable message cache items in %s", time.Now().Sub(t0).String())
	t0 = time.Now()
	if len(prune) == 0 {
		goto end
	}

	c.Lock()
	defer c.Unlock()

	for _, key := range prune {
		c._del(key)
	}
end:
	Log.Debugf("Pruned %d message cache items in %s. It now contains %d items, %d bytes",
		len(prune), time.Now().Sub(t0).String(), len(c.cache), c.size)
}
