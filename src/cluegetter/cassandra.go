// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"time"
	"math"
	"github.com/gocql/gocql"
)

type cqlQuery struct {
	query string
	args []interface{}
	retryCount int
	callbackSucces string
	callbackTempFailure string
	callbackFailure string
}

var cqlSess *gocql.Session;

var cqlQueryQueue chan *cqlQuery;

func cqlStart() {
	if Config.Cassandra.Enabled != true {
		Log.Info("Skipping Cassandra module because it was not enabled in the config")
		return
	}

	cluster := gocql.NewCluster(Config.Cassandra.Host...)
	cluster.Keyspace = Config.Cassandra.Keyspace
	cqlSessLocal, err := cluster.CreateSession();
	if err != nil {
		Log.Fatal(err);
	}

	cqlSess = cqlSessLocal

	cqlQueryQueue = make(chan *cqlQuery);

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
	cqlSess.Close()
	close(cqlQueryQueue)

	Log.Info("Closed Cassandra session");
}

func cqlQueryQueueHandler() {
	for {
		query, more := <-cqlQueryQueue
		if ! more {
			return
		}

		go cqlQueryQueueExecutor(query)
	}

}

func cqlQueryQueueExecutor(query cqlQuery) {
	if err := cqlSess.Query(query.query, query.args...); err != nil {
		if query.retryCount < 5 {
			query.retryCount = query.retryCount + 1;
			if query.callbackTempFailure != nil {
				query.callbackTempFailure()
			} else {
				Log.Error("Error while executing Cassandra query '%s', retrying. Error: %s",
					query.query, err)
			}

			time.Sleep(math.Pow(query.retryCount*100, 2) * time.Millisecond)
			cqlQueryQueueExecutor(query)
		} else if query.callbackFailure != nil {
			query.callbackFailure()
		} else {
			Log.Error("Error while executing Cassandra query '%s'. Error: %s", query.query, err)
		}
	}

	if query.callbackSucces != nil {
		query.callbackSucces()
	}
}
