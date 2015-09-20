// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type milterSession struct {
	id        uint64
	timeStart time.Time
	timeEnd   time.Time
	messages  []*milterMessage

	SaslUsername  string
	SaslSender    string
	SaslMethod    string
	CertIssuer    string
	CertSubject   string
	CipherBits    string
	Cipher        string
	TlsVersion    string
	Ip            string
	ReverseDns    string
	Hostname      string
	Helo          string
	MtaHostName   string
	MtaDaemonName string
}

type milterSessionWhitelistRange struct {
	ipStart net.IP
	ipEnd   net.IP
	mask    int
}

type milterSessionCluegetterClient struct {
	id         uint64
	hostname   string
	daemonName string
}

type milterSessionCluegetterClients struct {
	sync.Mutex
	clients []*milterSessionCluegetterClient
}

var milterSessionInsertStmt = *new(*sql.Stmt)
var milterSessionUpdateStmt = *new(*sql.Stmt)
var milterCluegetterClientInsertStmt = *new(*sql.Stmt)
var milterSessionWhitelist []*milterSessionWhitelistRange
var milterSessionClients milterSessionCluegetterClients

func milterSessionPrepStmt() {
	stmt, err := Rdbms.Prepare(`
		INSERT INTO session(cluegetter_instance, cluegetter_client, date_connect, date_disconnect, ip, reverse_dns, sasl_username)
			VALUES(?, ?, ?, NULL, ?, ?, ?)
	`)
	if err != nil {
		Log.Fatal(err)
	}

	milterSessionInsertStmt = stmt

	stmt, err = Rdbms.Prepare(`
		UPDATE session SET ip=?, reverse_dns=?, sasl_username=?, sasl_method=?, cert_issuer=?,
		                   cert_subject=?, cipher_bits=?, cipher=?, tls_version=?, date_disconnect=?
		   WHERE id=?`)
	if err != nil {
		Log.Fatal(err)
	}

	milterSessionUpdateStmt = stmt

	stmt, err = Rdbms.Prepare(`
		INSERT INTO cluegetter_client (hostname, daemon_name) VALUES(?,?)
			ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`)
	if err != nil {
		Log.Fatal(err)
	}

	milterCluegetterClientInsertStmt = stmt
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

func (s *milterSession) getReverseDns() string {
	return s.ReverseDns
}

func (s *milterSession) getHostname() string {
	return s.Hostname
}

func (s *milterSession) getHelo() string {
	return s.Helo
}

func (s *milterSession) getMtaHostName() string {
	return s.MtaHostName
}

func (s *milterSession) getMtaDaemonName() string {
	return s.MtaDaemonName
}

func (s *milterSession) isWhitelisted() bool {
	testIP := net.ParseIP(s.getIp()).To16()
	for _, whitelistRange := range milterSessionWhitelist {
		if bytes.Compare(testIP, whitelistRange.ipStart) >= 0 &&
			bytes.Compare(testIP, whitelistRange.ipEnd) <= 0 {
			return true
		}
	}

	return false
}

func milterSessionStart() {
	milterSessionPrepStmt()

	milterSessionWhitelist = make([]*milterSessionWhitelistRange, len(Config.ClueGetter.Whitelist))
	for idx, ipString := range Config.ClueGetter.Whitelist {
		if !strings.Contains(ipString, "/") {
			if strings.Contains(ipString, ":") {
				ipString = ipString + "/128"
			} else {
				ipString = ipString + "/32"
			}
		}
		_, ip, err := net.ParseCIDR(ipString)
		if ip == nil || err != nil {
			panic(fmt.Sprintf("Invalid whitelist ip specified '%s': %s", ipString, err))
		}

		ipEnd := make([]byte, len(ip.IP))
		for k, v := range ip.IP {
			ipEnd[k] = v | (ip.Mask[k] ^ 0xff)
		}

		mask, _ := strconv.Atoi(ipString[strings.Index(ipString, "/")+1:])
		milterSessionWhitelist[idx] = &milterSessionWhitelistRange{ip.IP.To16(), net.IP(ipEnd).To16(), mask}
	}

	Log.Info("Milter Session module started successfully")
}

func (s *milterSession) persist() {
	revDns := s.getReverseDns()
	if revDns == "unknown" {
		revDns = ""
	}

	client := milterSessionGetClient(s.getMtaHostName(), s.getMtaDaemonName())

	StatsCounters["RdbmsQueries"].increase(1)
	if s.id == 0 {
		res, err := milterSessionInsertStmt.Exec(
			instance, client.id, time.Now(), s.getIp(), revDns, s.getSaslUsername(),
		)
		if err != nil {
			panic("Could not execute milterSessionInsertStmt in milterSession.persist(): " + err.Error())
		}

		id, err := res.LastInsertId()
		if err != nil {
			panic("Could not get lastinsertid from milterSessionInsertStmt in milterSession.persist(): " + err.Error())
		}

		s.id = uint64(id)
	}

	_, err := milterSessionUpdateStmt.Exec(
		s.getIp(), revDns, s.getSaslUsername(), s.getSaslMethod(), s.getCertIssuer(), s.getCertSubject(),
		s.getCipherBits(), s.getCipher(), s.getTlsVersion(), s.timeEnd, s.getId())
	if err != nil {
		panic("Could not execute milterSessionUpdateStmt in milterSession.persist(): " + err.Error())
	}
}

func milterSessionGetClient(hostname string, daemonName string) *milterSessionCluegetterClient {
	milterSessionClients.Lock()
	defer milterSessionClients.Unlock()

	for _, client := range milterSessionClients.clients {
		if client.hostname == hostname && client.daemonName == daemonName {
			return client
		}
	}

	res, err := milterCluegetterClientInsertStmt.Exec(hostname, daemonName)
	if err != nil {
		panic("Could not insert new Cluegetter Client: " + err.Error())
	}

	id, err := res.LastInsertId()
	if err != nil {
		panic("Could not get lastinsertid from milterCluegetterClientInsertStmt: " + err.Error())
	}

	client := &milterSessionCluegetterClient{uint64(id), hostname, daemonName}
	milterSessionClients.clients = append(milterSessionClients.clients, client)
	return client
}

/******** milterMessage **********/

type milterMessage struct {
	session *milterSession

	QueueId string
	From    string
	Rcpt    []string
	Headers []*MessageHeader
	Body    []string

	injectMessageId string
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

func (m *milterMessage) setInjectMessageId(id string) {
	m.injectMessageId = id
}

func (m *milterMessage) getInjectMessageId() string {
	return m.injectMessageId
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
