// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"fmt"
	m "github.com/Freeaqingme/gomilter"
	"net"
	"sync"
	"time"
)

type milter struct {
	m.MilterRaw
}

type milterDataIndex struct {
	sessions map[uint64]*milterSession
	mu       sync.RWMutex
}

func (di *milterDataIndex) getNewSession() *milterSession {
	di.mu.Lock()
	defer di.mu.Unlock()

	sess := &milterSession{timeStart: time.Now()}
	sess.persist()
	di.sessions[sess.getId()] = sess
	return sess
}

var MilterDataIndex milterDataIndex

func milterStart() {
	MilterDataIndex = milterDataIndex{sessions: make(map[uint64]*milterSession)}

	StatsCounters["MilterCallbackConnect"] = &StatsCounter{}
	StatsCounters["MilterCallbackHelo"] = &StatsCounter{}
	StatsCounters["MilterCallbackEnvFrom"] = &StatsCounter{}
	StatsCounters["MilterCallbackEnvRcpt"] = &StatsCounter{}
	StatsCounters["MilterCallbackHeader"] = &StatsCounter{}
	StatsCounters["MilterProtocolErrors"] = &StatsCounter{}

	milter := new(milter)
	milter.FilterName = "GlueGetter"
	milter.Debug = false
	milter.Flags = m.ADDHDRS | m.ADDRCPT | m.CHGFROM | m.CHGBODY
	milter.Socket = "inet:10033@127.0.0.1" // Todo: Should be configurable

	go func() {
		out := m.Run(milter)
		Log.Info(fmt.Sprintf("Milter stopped. Exit code: %d", out))
		if out == -1 {
			// Todo: May just want to retry?
			Log.Fatal("libmilter returned an error.")
		}
	}()

	Log.Info("Milter module started")
}

func milterStop() {
	m.Stop()
}

func (milter *milter) Connect(ctx uintptr, hostname string, ip net.IP) (sfsistat int8) {
	sess := MilterDataIndex.getNewSession()
	sess.Hostname = hostname
	sess.Ip = ip.String()
	sess.persist()
	m.SetPriv(ctx, sess.getId())

	StatsCounters["MilterCallbackConnect"].increase(1)
	Log.Debug("%d Milter.Connect() called: ip = %s, hostname = %s", sess.getId(), ip, hostname)

	return m.Continue
}

func (milter *milter) Helo(ctx uintptr, helo string) (sfsistat int8) {
	sess := milterGetSession(ctx, true)
	if sess == nil { // This just doesn't seem to be supported by libmilter :(
		StatsCounters["MilterProtocolErrors"].increase(1)
		m.SetReply(ctx, "421", "4.7.0", "HELO/EHLO can only be specified at start of session")
		Log.Info("Received HELO/EHLO midway conversation. status=Tempfail rcode=421 xcode=4.7.0 ip=%s",
			m.GetSymVal(ctx, "{client_addr}"))
		return m.Tempfail
	}

	StatsCounters["MilterCallbackHelo"].increase(1)
	Log.Debug("%d Milter.Helo() called: helo = %s", sess.getId(), helo)

	sess.Helo = helo
	sess.CertIssuer = m.GetSymVal(ctx, "{cert_issuer}")
	sess.CertSubject = m.GetSymVal(ctx, "{cert_subject}")
	sess.CipherBits = m.GetSymVal(ctx, "{cipher_bits}")
	sess.Cipher = m.GetSymVal(ctx, "{cipher}")
	sess.TlsVersion = m.GetSymVal(ctx, "{tls_version}")
	sess.persist()

	return
}

func (milter *milter) EnvFrom(ctx uintptr, from []string) (sfsistat int8) {
	d := milterGetSession(ctx, true)
	msg := d.getNewMessage()

	StatsCounters["MilterCallbackEnvFrom"].increase(1)
	Log.Debug("%d Milter.EnvFrom() called: from = %s", d.getId(), from[0])

	if len(from) != 1 {
		StatsCounters["MilterProtocolErrors"].increase(1)
		Log.Critical("%d Milter.EnvFrom() callback received %d elements: %s", d.getId(), len(from), fmt.Sprint(from))
	}
	msg.From = from[0]
	return
}

func (milter *milter) EnvRcpt(ctx uintptr, rcpt []string) (sfsistat int8) {
	d := milterGetSession(ctx, true)
	msg := d.getLastMessage()
	msg.Rcpt = append(msg.Rcpt, rcpt[0])

	StatsCounters["MilterCallbackEnvRcpt"].increase(1)
	Log.Debug("%d Milter.EnvRcpt() called: rcpt = %s", d.getId(), fmt.Sprint(rcpt))
	return
}

func (milter *milter) Header(ctx uintptr, headerf, headerv string) (sfsistat int8) {
	var header MessageHeader
	header = &milterMessageHeader{headerf, headerv}

	d := milterGetSession(ctx, true)
	msg := d.getLastMessage()
	msg.Headers = append(msg.Headers, &header)

	StatsCounters["MilterCallbackHeader"].increase(1)
	Log.Debug("%d Milter.Header() called: header %s = %s", d.getId(), headerf, headerv)
	return
}

func (milter *milter) Eoh(ctx uintptr) (sfsistat int8) {
	d := milterGetSession(ctx, true)
	d.SaslSender = m.GetSymVal(ctx, "{auth_author}")
	d.SaslMethod = m.GetSymVal(ctx, "{auth_type}")
	d.SaslUsername = m.GetSymVal(ctx, "{auth_authen}")
	msg := d.getLastMessage()
	msg.QueueId = m.GetSymVal(ctx, "i")

	Log.Debug("%d milter.Eoh() was called", d.getId())
	return
}

func (milter *milter) Body(ctx uintptr, body []byte) (sfsistat int8) {
	bodyStr := string(body)

	s := milterGetSession(ctx, true)
	msg := s.getLastMessage()
	msg.Body = append(msg.Body, bodyStr)

	Log.Debug("%d milter.Body() was called. Length of body: %d", s.getId(), len(bodyStr))
	return
}

func (milter *milter) Eom(ctx uintptr) (sfsistat int8) {
	s := milterGetSession(ctx, true)
	Log.Debug("%d milter.Eom() was called", s.getId())

	messageGetVerdict(s.getLastMessage())
	return
}

func (milter *milter) Abort(ctx uintptr) (sfsistat int8) {
	Log.Debug("milter.Abort() was called")
	milterGetSession(ctx, false)

	return
}

func (milter *milter) Close(ctx uintptr) (sfsistat int8) {
	s := milterGetSession(ctx, false)
	Log.Debug("%d milter.Close() was called", s.getId())

	s.timeEnd = time.Now()
	s.persist()

	return
}

func milterLog(i ...interface{}) {
	Log.Debug(fmt.Sprintf("%s", i[:1]), i[1:]...)
}

func milterGetSession(ctx uintptr, keep bool) *milterSession {
	var u uint64
	m.GetPriv(ctx, &u)
	if keep {
		m.SetPriv(ctx, u)
	}

	return MilterDataIndex.sessions[u]
}
