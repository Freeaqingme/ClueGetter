// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"sync"
)

var StatsControl = make(chan struct{})
var StatsCounters = make(map[string]*StatsCounter)
var StatsCountersMutex = &sync.Mutex{}

type statsDatapoint struct {
	timestamp int64 // time.Now().Nanoseconds()
	value     int32
}

type StatsCounter struct {
	mu           sync.Mutex
	dataPoints   []*statsDatapoint
	total        int32
	ignore_prune bool
}

func (s *StatsCounter) increase(value int32) {
	return
}

func statsInitCounter(name string) {
	StatsCountersMutex.Lock()
	defer StatsCountersMutex.Unlock()

	StatsCounters[name] = &StatsCounter{}
}
