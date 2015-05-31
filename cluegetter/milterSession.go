// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"strings"
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

func (m *milterMessage) getBody() string {
	return strings.Join(m.Body, "")
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
