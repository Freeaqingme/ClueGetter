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
	id        [16]byte
	timeStart time.Time
	timeEnd   time.Time
	messages  []*Message

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

	persisted bool
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
var milterCluegetterClientInsertStmt = *new(*sql.Stmt)
var milterSessionWhitelist []*milterSessionWhitelistRange
var milterSessionClients milterSessionCluegetterClients

func milterSessionPrepStmt() {
	stmt, err := Rdbms.Prepare(`
		INSERT INTO session(id, cluegetter_instance, cluegetter_client, date_connect,
							date_disconnect, ip, reverse_dns, helo, sasl_username,
							sasl_method, cert_issuer, cert_subject, cipher_bits, cipher, tls_version)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		Log.Fatal(err)
	}

	milterSessionInsertStmt = stmt

	stmt, err = Rdbms.Prepare(`
		INSERT INTO cluegetter_client (hostname, daemon_name) VALUES(?,?)
			ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`)
	if err != nil {
		Log.Fatal(err)
	}

	milterCluegetterClientInsertStmt = stmt
}

func (s *milterSession) getNewMessage() *Message {
	msg := &Message{}
	msg.session = s

	s.messages = append(s.messages, msg)
	return msg
}

func (s *milterSession) getLastMessage() *Message {
	return s.messages[len(s.messages)-1]
}

func (s *milterSession) getId() [16]byte {
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

	s.persisted = true
	id := s.getId()

	StatsCounters["RdbmsQueries"].increase(1)
	_, err := milterSessionInsertStmt.Exec(
		string(id[:]), instance, client.id, s.timeStart, s.timeEnd, s.getIp(), revDns, s.getHelo(),
		s.getSaslUsername(), s.getSaslMethod(), s.getCertIssuer(), s.getCertSubject(),
		s.getCipherBits(), s.getCipher(), s.getTlsVersion(),
	)
	if err != nil {
		panic("Could not execute milterSessionInsertStmt in milterSession.persist(): " + err.Error())
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

type Message struct {
	session *milterSession

	QueueId string
	From    string
	Rcpt    []string
	Headers []*MessageHeader
	Body    []byte

	injectMessageId string
}

/******** milterMessageHeader ********/

type MessageHeader struct {
	Key   string
	Value string
}

func (h *MessageHeader) getKey() string {
	return h.Key
}

func (h *MessageHeader) getValue() string {
	return h.Value
}
