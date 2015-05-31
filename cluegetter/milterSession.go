// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"time"
)

type milterSession struct {
	id        string
	timeStart time.Time
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

type milterMessage struct {
	session *milterSession

	QueueId string
	From    string
	Rcpt    []string
	Header  []*milterMessageHeader
	Body    []string
}

type milterMessageHeader struct {
	Key   string
	Value string
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

func (s *milterSession) getId() string {
	return s.id
}
