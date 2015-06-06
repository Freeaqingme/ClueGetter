// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"database/sql"
	"strings"
	"sync"
	"time"
)

const (
	messagePermit = iota
	messageTempFail
	messageReject
)

type Session interface {
	getId() uint64
	getSaslUsername() string
	getSaslSender() string
	getSaslMethod() string
	getCertIssuer() string
	getCertSubject() string
	getCipherBits() string
	getCipher() string
	getTlsVersion() string
	getIp() string
	getHostname() string
	getHelo() string
}

type Message interface {
	getSession() *Session
	getHeaders() []*MessageHeader

	getQueueId() string
	getFrom() string
	getRcptCount() int
	getRecipients() []string
	getBody() string
}

type MessageHeader interface {
	getKey() string
	getValue() string
}

type MessageCheckResult struct {
	module          string
	suggestedAction int
	message         string
	score           float64
}

var MessageInsertMsgStmt = *new(*sql.Stmt)
var MessageInsertRcptStmt = *new(*sql.Stmt)
var MessageInsertMsgRcptStmt = *new(*sql.Stmt)
var MessageInsertMsgHdrStmt = *new(*sql.Stmt)
var MessageSetVerdictStmt = *new(*sql.Stmt)
var MessageInsertModuleResultStmt = *new(*sql.Stmt)

func messageStart() {
	stmt, err := Rdbms.Prepare(`INSERT INTO message (id, session, date, sender, body, rcpt_count)
								VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertMsgStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT INTO recipient(local, domain) VALUES(?, ?)
								ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertRcptStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT INTO message_recipient(message, recipient) VALUES(?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertMsgRcptStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT INTO message_header(message, name, body) VALUES(?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertMsgHdrStmt = stmt

	stmt, err = Rdbms.Prepare(`UPDATE message SET verdict=?, verdict_msg=?, rejectScore=?, tempfailScore=? WHERE id=?`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageSetVerdictStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT INTO message_result (message, module, verdict, score) VALUES(?, ?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertModuleResultStmt = stmt

	Log.Info("Message handler started successfully")
}

func messageStop() {
	MessageInsertMsgStmt.Close()
	Log.Info("Message handler stopped successfully")
}

func messageGetVerdict(msg Message) (int, string) {
	messageSave(msg)

	var results [3][]*MessageCheckResult
	results[messagePermit] = make([]*MessageCheckResult, 0)
	results[messageTempFail] = make([]*MessageCheckResult, 0)
	results[messageReject] = make([]*MessageCheckResult, 0)

	var totalScores [3]float64

	verdictValue := [3]string{"permit", "tempfail", "reject"}
	for result := range messageGetResults(msg) {
		results[result.suggestedAction] = append(results[result.suggestedAction], result)
		totalScores[result.suggestedAction] += result.score

		_, err := MessageInsertModuleResultStmt.Exec(
			msg.getQueueId(), result.module, verdictValue[result.suggestedAction], result.score)
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
		}
	}

	getMessage := func(results []*MessageCheckResult) string {
		out := results[0].message
		maxScore := float64(0)
		for _, result := range results {
			if result.score > maxScore && result.message != "" {
				out = result.message
				maxScore = result.score
			}
		}

		return out
	}

	if totalScores[messageReject] > Config.ClueGetter.Message_Reject_Score {
		verdictMsg := getMessage(results[messageReject])
		messageSaveVerdict(msg, messageReject, verdictMsg, totalScores[messageReject], totalScores[messageTempFail])
		return messageReject, verdictMsg
	}
	if (totalScores[messageTempFail] + totalScores[messageReject]) > Config.ClueGetter.Message_Tempfail_Score {
		verdictMsg := getMessage(results[messageTempFail])
		messageSaveVerdict(msg, messageTempFail, verdictMsg, totalScores[messageReject], totalScores[messageTempFail])
		return messageTempFail, verdictMsg
	}

	messageSaveVerdict(msg, messagePermit, "", totalScores[messageReject], totalScores[messageTempFail])
	return messagePermit, ""
}

func messageSaveVerdict(msg Message, verdict int, verdictMsg string, rejectScore float64, tempfailScore float64) {
	verdictValue := [3]string{"permit", "tempfail", "reject"}

	StatsCounters["RdbmsQueries"].increase(1)
	_, err := MessageSetVerdictStmt.Exec(
		verdictValue[verdict],
		verdictMsg,
		rejectScore,
		tempfailScore,
		msg.getQueueId(),
	)

	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
	}
}

func messageGetResults(msg Message) chan *MessageCheckResult {
	var wg sync.WaitGroup
	out := make(chan *MessageCheckResult)

	if Config.Quotas.Enabled {
		wg.Add(1)
		go func() {
			out <- quotasIsAllowed(msg)
			wg.Done()
		}()
	}
	if Config.SpamAssassin.Enabled {
		wg.Add(1)
		go func() {
			out <- saGetResult(msg)
			wg.Done()
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func messageSave(msg Message) {
	sess := *msg.getSession()

	StatsCounters["RdbmsQueries"].increase(1)
	_, err := MessageInsertMsgStmt.Exec(
		msg.getQueueId(),
		sess.getId(),
		time.Now(),
		msg.getFrom(),
		msg.getBody(),
		msg.getRcptCount(),
	)

	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
	}

	messageSaveRecipients(msg.getRecipients(), msg.getQueueId())
	messageSaveHeaders(msg)
}

func messageSaveRecipients(recipients []string, msgId string) {
	for _, rcpt := range recipients {
		var local string
		var domain string

		if strings.Index(rcpt, "@") != -1 {
			local = strings.SplitN(rcpt, "@", 2)[0]
			domain = strings.SplitN(rcpt, "@", 2)[1]
		} else {
			local = rcpt
			domain = ""
		}

		StatsCounters["RdbmsQueries"].increase(1)
		res, err := MessageInsertRcptStmt.Exec(local, domain)
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
		}

		rcptId, err := res.LastInsertId()
		if err != nil {
			Log.Fatal(err)
		}

		StatsCounters["RdbmsQueries"].increase(1)
		_, err = MessageInsertMsgRcptStmt.Exec(msgId, rcptId)
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
		}
	}
}

func messageSaveHeaders(msg Message) {
	for _, headerPair := range msg.getHeaders() {
		StatsCounters["RdbmsQueries"].increase(1)
		_, err := MessageInsertMsgHdrStmt.Exec(
			msg.getQueueId(), (*headerPair).getKey(), (*headerPair).getValue())

		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
		}
	}
}
