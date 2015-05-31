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
)

type Session interface {
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
	getBody() string
}

type MessageHeader interface {
	getKey() string
	getValue() string
}

var MessageInsertMsgStmt = *new(*sql.Stmt)

func messageStart() {
	stmt, err := Rdbms.Prepare(`
		INSERT INTO message (id, date, count, last_protocol_state, sender, recipient, client_address, sasl_username)
		VALUES (?, now(), ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY
		UPDATE count=?, last_protocol_state=?, sender=?, recipient=?, client_address=?, sasl_username=?`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertMsgStmt = stmt

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
		msg.getQueueId(), // Can a message never have NOQUEUE?!
		msg.getRcptCount(),
		"REMOVEME", // PROTOCOL STATE
		msg.getFrom(),
		"", // Recipient
		sess.getIp(),
		sess.getSaslUsername(),
		msg.getRcptCount(),
		"", // PROTOCOL STATE
		msg.getFrom(),
		"", // Rcpt
		sess.getIp(),
		sess.getSaslUsername(),
	)

	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
	}
}
