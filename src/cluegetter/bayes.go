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
	"net"
	"strings"
	"time"
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
	err := redisPublish(fmt.Sprintf("cluegetter!!bayes!learn"), payloadJson)
	if err != nil {
		Log.Error("Error while reporting bayes message id: %s", err.Error())
	}
}

func bayesAddToCorpus(spam bool, msg *Proto_MessageV1, messageId, host, reporter, reason string) {
	// TODO
}

func bayesReportMessageId(spam bool, messageId, host, reporter, reason string) {
	cluegetterRecover("bayesReportMessageId")
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
	key := fmt.Sprintf("cluegetter!%d!bayes!reportMessageId", instance)
	err := redisPublish(key, payloadJson)

	if err != nil {
		Log.Error("Error while reporting bayes message id: %s", err.Error())
	}
}

func bayesLearn(item string) {
	cluegetterRecover("bayesHandleReportMessageIdQueueItem")

	var dat map[string]string
	err := json.Unmarshal([]byte(item), &dat)
	if err != nil {
		Log.Error("Could not unmarshal map in bayesLearn(): %s", err.Error())
		return
	}

	msgBytes := messagePersistCache.getByMessageId(dat["messageId"])
	dat["message"] = string(msgBytes)

	msg, err := messagePersistUnmarshalProto(msgBytes)
	if err != nil {
		Log.Error("Could not unmarshal message in bayesLearn(): %s", err.Error())
		return
	}

	saLearn(msg, dat["spam"] == "spam")
}

// This shows the disadvantage of having both a Message and Proto_MessageV1
// object. We really should look into merging the Message and Proto_ objects
// and subsequently merge this with: func (msg *Message) String() []byte
func bayesRenderProtoMsg(msg *Proto_MessageV1) []byte {
	sess := *msg.Session
	fqdn := sess.Hostname
	revdns, err := net.LookupAddr(*sess.Ip)
	revdnsStr := "unknown"
	if err == nil {
		revdnsStr = strings.Join(revdns, "")
	}

	body := make([]string, 0)

	body = append(body, fmt.Sprintf("Received: from %s (%s [%s])\r\n\tby %s with SMTP id %s; %s\r\n",
		*sess.Helo,
		revdnsStr,
		*sess.Ip,
		*fqdn,
		*msg.Id,
		time.Now().Format(time.RFC1123Z)))

	for _, header := range msg.Headers {
		body = append(body, *(header).Key+": "+*(header).Value+"\r\n")
	}

	body = append(body, "\r\n")
	body = append(body, string(msg.Body))

	return []byte(strings.Join(body, ""))
}
