// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"expvar"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

var StatsControl = make(chan struct{})
var StatsCounters = make(map[string]*StatsCounter)
var StatsHttpListener *net.TCPListener

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
	s.mu.Lock()
	defer s.mu.Unlock()

	dataPoint := &statsDatapoint{time.Now().UnixNano(), value}
	s.dataPoints = append(s.dataPoints, dataPoint)
	s.total += value
}

func (s *StatsCounter) decrease(value int32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dataPoint := &statsDatapoint{time.Now().UnixNano(), value}
	s.dataPoints = append(s.dataPoints, dataPoint)
	s.total -= value
}

func (s *StatsCounter) getTotalCounter( /* period */ ) int32 { //Todo: Require argument period
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.total
}

func (s *StatsCounter) prune(name string) {
	Log.Debug("Pruning %s", name)
	s.mu.Lock()
	defer s.mu.Unlock()

	keepCount := 0
	pruneCount := 0

	s.total = 0
	prunedDataPoints := []*statsDatapoint{}
	pruneThreshold := time.Now().UnixNano() - (90 * 10e9) // 10e9 = 10 seconds

	for _, value := range s.dataPoints {
		if value.timestamp > pruneThreshold {
			s.total += value.value
			prunedDataPoints = append(prunedDataPoints, value)
			keepCount += 1
		} else {
			pruneCount += 1
		}
	}

	Log.Debug("Pruned %d data points from %s, kept %d data points", pruneCount, name, keepCount)

	s.dataPoints = prunedDataPoints
}

func statsStart() {

	statsHttpStart()
	go statsLog()
	go statsPrune()

	expvar.Publish("statscounters", expvar.Func(statsPublish))
}

func statsPublish() interface{} {
	out := map[string]int32{}

	for key := range StatsCounters {
		out[key] = StatsCounters[key].getTotalCounter()
	}

	return out
}

func statsLog() {
	ticker := time.NewTicker(60 * time.Second)

	for {
		select {
		case <-ticker.C:
			for key := range StatsCounters {
				Log.Debug("%s: %d", key, StatsCounters[key].getTotalCounter())
			}
		case <-StatsControl:
			ticker.Stop()
			return
		}
	}
}

func statsPrune() {
	ticker := time.NewTicker(900 * time.Second)

	for {
		select {
		case <-ticker.C:
			for key := range StatsCounters {
				if StatsCounters[key].ignore_prune == true {
					continue
				}
				StatsCounters[key].prune(key)
			}
		case <-StatsControl:
			ticker.Stop()
			return
		}
	}

}

func statsHttpStart() {
	listen_host := Config.ClueGetter.Stats_Http_Listen_Host
	listen_port := Config.ClueGetter.Stats_Http_Listen_Port

	laddr, err := net.ResolveTCPAddr("tcp", listen_host+":"+listen_port)
	if nil != err {
		log.Fatalln(err)
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if nil != err {
		log.Fatalln(err)
	}
	Log.Info("Stats HTTP interface now listening on %s", listener.Addr())

	http.HandleFunc("/", handler)
	go http.Serve(listener, nil)

	StatsHttpListener = listener
}

func statsStop() {
	close(StatsControl)
	StatsHttpListener.Close()
	Log.Info("Stats HTTP Listener closed")
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Path: %s", r.URL.Path[1:])
}
