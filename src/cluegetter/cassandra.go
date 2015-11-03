// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"github.com/gocql/gocql"
	"math"
	"time"
)

type cqlQuery struct {
	query               string
	args                []interface{}
	retryCount          int
	callbackSucces      func(*cqlQuery)
	callbackFailure     func(*cqlQuery)
	callbackTempFailure func(*cqlQuery)
}

var cqlSess *gocql.Session

var cqlQueryQueue chan *cqlQuery

func cqlStart() {
	if Config.Cassandra.Enabled != true {
		Log.Info("Skipping Cassandra module because it was not enabled in the config")
		return
	}

	cluster := gocql.NewCluster(Config.Cassandra.Host...)
	cluster.Keyspace = Config.Cassandra.Keyspace
	cqlSessLocal, err := cluster.CreateSession()
	if err != nil {
		Log.Fatal("Error connecting with Cassandra: ", err)
	}

	cqlSess = cqlSessLocal

	cqlQueryQueue = make(chan *cqlQuery)

	go cqlQueryQueueHandler()

	/*	var message, instance,body string
		var date time.Time

		iter := session.Query("SELECT message, instance, date,body FROM message").Iter()
		for iter.Scan(&message, &instance, &date, &body) {
			fmt.Println(message, instance, date, body)
		}
		if err := iter.Close(); err != nil {
			log.Fatal(err)
		}

	*/
	Log.Info("Successfully connected to Cassandra")
}

func cqlStop() {
	if !Config.Cassandra.Enabled {
		return
	}

	cqlSess.Close()
	close(cqlQueryQueue)

	Log.Info("Closed Cassandra session")
}

func cqlQueryQueueHandler() {
	for {
		query, more := <-cqlQueryQueue
		if !more {
			return
		}

		go cqlQueryQueueExecutor(query)
	}
}

func cqlQueryQueueExecutor(query *cqlQuery) {
	var err error
	if err = cqlSess.Query(query.query, query.args...).Exec(); err == nil {
		if query.callbackSucces != nil {
			query.callbackSucces(query)
		}
		return
	}

	if query.retryCount > 4 {
		if query.callbackFailure != nil {
			query.callbackFailure(query)
		} else {
			Log.Error("Error while executing Cassandra query '%s'. Error: %s", query.query, err)
		}
		return
	}

	if query.callbackTempFailure != nil {
		query.callbackTempFailure(query)
	} else {
		Log.Warning("Error while executing Cassandra query '%s', retrying. Error: %s",
			query.query, err)
	}

	time.Sleep(time.Duration(math.Pow(float64(query.retryCount*10), 2.5)) * time.Millisecond)
	query.retryCount = query.retryCount + 1
	cqlQueryQueueExecutor(query)
}
