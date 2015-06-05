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
	"fmt"
	"strings"
	"time"
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

var MessageInsertMsgStmt = *new(*sql.Stmt)
var MessageInsertRcptStmt = *new(*sql.Stmt)
var MessageInsertMsgRcptStmt = *new(*sql.Stmt)
var MessageInsertMsgHdrStmt = *new(*sql.Stmt)

func messageStart() {
	stmt, err := Rdbms.Prepare(`INSERT INTO message (id, session, date, sender, rcpt_count)
								VALUES (?, ?, ?, ?, ?)`)
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

	Log.Info("Message handler started successfully")
}

func messageStop() {
	MessageInsertMsgStmt.Close()
	Log.Info("Message handler stopped successfully")
}

func messageGetVerdict(msg Message) {
	messageSave(msg)

	if Config.Quotas.Enabled {
		fmt.Println(quotasIsAllowed(msg))
	}

}

func messageSave(msg Message) {
	sess := *msg.getSession()

	StatsCounters["RdbmsQueries"].increase(1)
	_, err := MessageInsertMsgStmt.Exec(
		msg.getQueueId(),
		sess.getId(),
		time.Now(),
		msg.getFrom(),
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
