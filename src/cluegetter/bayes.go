// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"encoding/json"
	"fmt"
)

func init() {
	reportMessageId := make(chan string, 64)
	learnMessage := make(chan string, 64)
	enable := func() bool { return Config.Bayes.Enabled }
	init := func() {
		bayesStart(reportMessageId, learnMessage)
	}

	ModuleRegister(&module{
		name:   "bayes",
		enable: &enable,
		init:   &init,
		rpc: map[string]chan string{
			"bayes!reportMessageId": reportMessageId,
			"bayes!learn":           learnMessage,
		},
		// TODO: HTTP Interface to report HAM/SPAM
	})
}

func bayesStart(reportMessageId, learnMessage chan string) {
	go bayesHandleReportMessageIdQueue(reportMessageId)
	go bayesHandleLearnQueue(learnMessage)
}

func bayesHandleReportMessageIdQueue(reportMessageIdQueue chan string) {
	for report := range reportMessageIdQueue {
		go bayesHandleReportMessageIdQueueItem(report)
	}
}

func bayesHandleLearnQueue(learnMessageQueue chan string) {
	for lesson := range learnMessageQueue {
		go bayesLearn(lesson)
	}
}

func bayesHandleReportMessageIdQueueItem(item string) {
	cluegetterRecover("bayesHandleReportMessageIdQueueItem")

	var dat map[string]string
	json.Unmarshal([]byte(item), &dat)

	msgBytes := messagePersistCache.getByMessageId(dat["messageId"])
	dat["message"] = string(msgBytes)

	msg, _ := messagePersistUnmarshalProto(msgBytes)
	if dat["spam"] == "yes" {
		bayesAddToCorpus(true, msg, dat["messageId"], dat["host"], dat["reporter"], dat["reason"])
	} else {
		bayesAddToCorpus(false, msg, dat["messageId"], dat["host"], dat["reporter"], dat["reason"])
	}

	payloadJson, _ := json.Marshal(dat)
	err := redisClient.Publish(fmt.Sprintf("cluegetter!!bayes!learn"),
		string(payloadJson)).Err()

	if err != nil {
		Log.Error("Error while reporting bayes message id: %s", err.Error())
	}
}

func bayesAddToCorpus(spam bool, msg *Proto_MessageV1, messageId, host, reporter, reason string) {
	// TODO
}

func bayesReportMessageId(spam bool, messageId, host, reporter, reason string) {
	if !Config.Bayes.Enabled {
		return
	}

	spamStr := "yes"
	if !spam {
		spamStr = "no"
	}
	payload := map[string]string{
		"messageId": messageId,
		"host":      host,
		"reporter":  reporter,
		"reason":    reason,
		"spam":      spamStr,
	}

	payloadJson, _ := json.Marshal(payload)
	err := redisClient.Publish(fmt.Sprintf("cluegetter!%d!bayes!reportMessageId", instance),
		string(payloadJson)).Err()

	if err != nil {
		Log.Error("Error while reporting bayes message id: %s", err.Error())
	}
}

func bayesLearn(item string) {
	cluegetterRecover("bayesHandleReportMessageIdQueueItem")

	var dat map[string]string
	json.Unmarshal([]byte(item), &dat)

	msgBytes := messagePersistCache.getByMessageId(dat["messageId"])
	dat["message"] = string(msgBytes)

	msg, _ := messagePersistUnmarshalProto(msgBytes)
	fmt.Println("Learning...", msg)

	// TODO
}
