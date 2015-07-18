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
	"sync"
	"time"
)

type milterSession struct {
	id        uint64
	timeStart time.Time
	timeEnd   time.Time
	messages  []*milterMessage

	SaslUsername string
	SaslSender   string
	SaslMethod   string
	CertIssuer   string
	CertSubject  string
	CipherBits   string
	Cipher       string
	TlsVersion   string
	Ip           string
	Hostname     string
	Helo         string
}

var milterSessionInsertStmt = *new(*sql.Stmt)
var milterSessionUpdateStmt = *new(*sql.Stmt)

func milterSessionPrepStmt() {
	stmt, err := Rdbms.Prepare(`
		INSERT INTO session(cluegetter_instance, date_connect, date_disconnect, ip, sasl_username)
			VALUES(?, ?, NULL, ?, ?)
	`)
	if err != nil {
		Log.Fatal(err)
	}

	milterSessionInsertStmt = stmt

	stmt, err = Rdbms.Prepare(`
		UPDATE session SET ip=?, sasl_username=?, date_disconnect=? WHERE id=?`)
	if err != nil {
		Log.Fatal(err)
	}

	milterSessionUpdateStmt = stmt
}

func (s *milterSession) getNewMessage() *milterMessage {
	msg := &milterMessage{}
	msg.session = s

	s.messages = append(s.messages, msg)
	return msg
}

func (s *milterSession) getLastMessage() *milterMessage {
	return s.messages[len(s.messages)-1]
}

func (s *milterSession) getId() uint64 {
	return s.id
}

func (s *milterSession) getSaslUsername() string {
	return s.SaslUsername
}

func (s *milterSession) getSaslSender() string {
	return s.SaslSender
}

func (s *milterSession) getSaslMethod() string {
	return s.SaslMethod
}

func (s *milterSession) getCertIssuer() string {
	return s.CertIssuer
}

func (s *milterSession) getCertSubject() string {
	return s.CertSubject
}

func (s *milterSession) getCipherBits() string {
	return s.CipherBits
}

func (s *milterSession) getCipher() string {
	return s.Cipher
}

func (s *milterSession) getTlsVersion() string {
	return s.TlsVersion
}

func (s *milterSession) getIp() string {
	return s.Ip
}

func (s *milterSession) getHostname() string {
	return s.Hostname
}

func (s *milterSession) getHelo() string {
	return s.Helo
}

func (s *milterSession) persist() {
	var once sync.Once
	once.Do(milterSessionPrepStmt)

	StatsCounters["RdbmsQueries"].increase(1)
	if s.id == 0 {
		res, err := milterSessionInsertStmt.Exec(instance, time.Now(), s.getIp(), s.getSaslUsername())
		if err != nil {
			panic("Could not execute milterSessionInsertStmt in milterSession.persist(): " + err.Error())
		}

		id, err := res.LastInsertId()
		if err != nil {
			panic("Could not get lastinsertid from milterSessionInsertStmt in milterSession.persist(): " + err.Error())
		}

		s.id = uint64(id)
	}

	_, err := milterSessionUpdateStmt.Exec(s.getIp(), s.getSaslUsername(), s.timeEnd, s.getId())
	if err != nil {
		panic("Could not execute milterSessionUpdateStmt in milterSession.persist(): " + err.Error())
	}
}

/******** milterMessage **********/

type milterMessage struct {
	session *milterSession

	QueueId string
	From    string
	Rcpt    []string
	Headers []*MessageHeader
	Body    []string
}

func (m *milterMessage) getHeaders() []*MessageHeader {
	return m.Headers
}

func (m *milterMessage) getSession() *Session {
	var session Session
	session = m.session
	return &session
}

func (m *milterMessage) getQueueId() string {
	return m.QueueId
}

func (m *milterMessage) getFrom() string {
	return m.From
}

func (m *milterMessage) getRcptCount() int {
	return len(m.Rcpt)
}

func (m *milterMessage) getRecipients() []string {
	return m.Rcpt
}

func (m *milterMessage) getBody() []string {
	return m.Body
}

/******** milterMessageHeader ********/

type milterMessageHeader struct {
	Key   string
	Value string
}

func (h *milterMessageHeader) getKey() string {
	return h.Key
}

func (h *milterMessageHeader) getValue() string {
	return h.Value
}
