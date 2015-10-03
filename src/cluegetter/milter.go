// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"fmt"
	m "github.com/Freeaqingme/gomilter"
	"net"
	"strings"
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

func (di *milterDataIndex) getNewSession(sess *milterSession) *milterSession {
	sess.persist()

	di.mu.Lock()
	defer di.mu.Unlock()

	di.sessions[sess.getId()] = sess
	return sess
}

func (di *milterDataIndex) delete(s *milterSession, lock bool) {
	s.timeEnd = time.Now()
	s.persist()

	if lock {
		di.mu.Lock()
		defer di.mu.Unlock()
	}
	delete(di.sessions, s.getId())
}

func (di *milterDataIndex) prune() {
	now := time.Now()
	di.mu.Lock()
	defer di.mu.Unlock()

	for k, v := range di.sessions {
		if now.Sub(v.timeStart).Minutes() > 15 {
			Log.Debug("Pruning session %d", k)
			di.delete(v, false)
		}
	}
}

var MilterDataIndex milterDataIndex

func milterStart() {
	MilterDataIndex = milterDataIndex{sessions: make(map[uint64]*milterSession)}

	statsInitCounter("MilterCallbackConnect")
	statsInitCounter("MilterCallbackHelo")
	statsInitCounter("MilterCallbackEnvFrom")
	statsInitCounter("MilterCallbackEnvRcpt")
	statsInitCounter("MilterCallbackHeader")
	statsInitCounter("MilterCallbackEoh")
	statsInitCounter("MilterCallbackBody")
	statsInitCounter("MilterCallbackEom")
	statsInitCounter("MilterCallbackAbort")
	statsInitCounter("MilterCallbackClose")
	statsInitCounter("MilterProtocolErrors")

	milter := new(milter)
	milter.FilterName = "ClueGetter"
	milter.Debug = false
	milter.Flags = m.ADDHDRS | m.ADDRCPT | m.CHGFROM | m.CHGBODY
	milter.Socket = Config.ClueGetter.Milter_Socket

	go func() {
		out := m.Run(milter)
		Log.Info(fmt.Sprintf("Milter stopped. Exit code: %d", out))
		if out == -1 {
			// Todo: May just want to retry?
			Log.Fatal("libmilter returned an error.")
		}
	}()

	go milterPrune()

	Log.Info("Milter module started")
}

func milterStop() {
	m.Stop()
}

func milterPrune() {
	ticker := time.NewTicker(900 * time.Second)

	for {
		select {
		case <-ticker.C:
			MilterDataIndex.prune()
		}
	}
}

func (milter *milter) Connect(ctx uintptr, hostname string, ip net.IP) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	sess := &milterSession{timeStart: time.Now()}
	sess.Hostname = hostname
	sess.Ip = ip.String()
	sess.ReverseDns = m.GetSymVal(ctx, "{client_ptr}")
	sess.MtaHostName = m.GetSymVal(ctx, "j")
	sess.MtaDaemonName = m.GetSymVal(ctx, "{daemon_name}")
	MilterDataIndex.getNewSession(sess)
	m.SetPriv(ctx, sess.getId())

	StatsCounters["MilterCallbackConnect"].increase(1)
	Log.Debug("%d Milter.Connect() called: ip = %s, hostname = %s", sess.getId(), ip, hostname)

	return m.Continue
}

func (milter *milter) Helo(ctx uintptr, helo string) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	sess := milterGetSession(ctx, true, true)
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
	defer milterHandleError(ctx, &sfsistat)

	d := milterGetSession(ctx, true, false)
	msg := d.getNewMessage()

	StatsCounters["MilterCallbackEnvFrom"].increase(1)
	Log.Debug("%d Milter.EnvFrom() called: from = %s", d.getId(), from[0])

	if len(from) == 0 {
		StatsCounters["MilterProtocolErrors"].increase(1)
		Log.Critical("%d Milter.EnvFrom() callback received %d elements", d.getId(), len(from))
		panic(fmt.Sprint("%d Milter.EnvFrom() callback received %d elements", d.getId(), len(from)))
	}
	msg.From = strings.ToLower(strings.Trim(from[0], "<>"))
	return
}

func (milter *milter) EnvRcpt(ctx uintptr, rcpt []string) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	d := milterGetSession(ctx, true, false)
	msg := d.getLastMessage()
	msg.Rcpt = append(msg.Rcpt, strings.ToLower(strings.Trim(rcpt[0], "<>")))

	StatsCounters["MilterCallbackEnvRcpt"].increase(1)
	Log.Debug("%d Milter.EnvRcpt() called: rcpt = %s", d.getId(), fmt.Sprint(rcpt))
	return
}

func (milter *milter) Header(ctx uintptr, headerf, headerv string) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	var header MessageHeader
	header = &milterMessageHeader{headerf, headerv}

	sess := milterGetSession(ctx, true, false)
	msg := sess.getLastMessage()
	msg.Headers = append(msg.Headers, &header)

	StatsCounters["MilterCallbackHeader"].increase(1)
	Log.Debug("%d Milter.Header() called: header %s = %s", sess.getId(), headerf, headerv)
	return
}

func (milter *milter) Eoh(ctx uintptr) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	sess := milterGetSession(ctx, true, false)
	sess.SaslSender = m.GetSymVal(ctx, "{auth_author}")
	sess.SaslMethod = m.GetSymVal(ctx, "{auth_type}")
	sess.SaslUsername = m.GetSymVal(ctx, "{auth_authen}")
	msg := sess.getLastMessage()
	msg.QueueId = m.GetSymVal(ctx, "i")
	sess.persist()

	StatsCounters["MilterCallbackEoh"].increase(1)
	Log.Debug("%d milter.Eoh() was called", sess.getId())
	return
}

func (milter *milter) Body(ctx uintptr, body []byte) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	bodyStr := string(body)

	s := milterGetSession(ctx, true, false)
	msg := s.getLastMessage()
	msg.Body = append(msg.Body, bodyStr)

	StatsCounters["MilterCallbackBody"].increase(1)
	Log.Debug("%d milter.Body() was called. Length of body: %d", s.getId(), len(bodyStr))
	return
}

func (milter *milter) Eom(ctx uintptr) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	s := milterGetSession(ctx, true, false)
	StatsCounters["MilterCallbackEom"].increase(1)
	Log.Debug("%d milter.Eom() was called", s.getId())

	verdict, msg, results := messageGetVerdict(s.getLastMessage())
	for _, hdr := range messageGetHeadersToAdd(s.getLastMessage(), results) {
		m.AddHeader(ctx, hdr.getKey(), hdr.getValue())
	}

	if s.isWhitelisted() {
		verdict = messagePermit
		msg = "Whitelisted"
	}

	switch {
	case verdict == messagePermit:
		Log.Info("Message Permit: sess=%d message=%s %s", s.getId(), s.getLastMessage().getQueueId(), msg)
		return
	case verdict == messageTempFail:
		m.SetReply(ctx, "421", "4.7.0", msg)
		Log.Info("Message TempFail: sess=%d message=%s msg: %s", s.getId(), s.getLastMessage().getQueueId(), msg)
		if Config.ClueGetter.Noop {
			return
		}
		return m.Tempfail
	case verdict == messageReject:
		m.SetReply(ctx, "550", "5.7.1", msg)
		Log.Info("Message Reject: sess=%d message=%s msg: %s", s.getId(), s.getLastMessage().getQueueId(), msg)
		if Config.ClueGetter.Noop {
			return
		}
		return m.Reject
	}

	panic("verdict was not recognized")
}

func (milter *milter) Abort(ctx uintptr) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	StatsCounters["MilterCallbackAbort"].increase(1)
	Log.Debug("milter.Abort() was called")
	milterGetSession(ctx, true, true)

	return
}

func (milter *milter) Close(ctx uintptr) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	StatsCounters["MilterCallbackClose"].increase(1)
	s := milterGetSession(ctx, false, true)
	if s == nil {
		Log.Debug("%d milter.Close() was called. No context supplied")
		return
	}
	Log.Debug("%d milter.Close() was called", s.getId())

	MilterDataIndex.delete(s, true)
	return
}

func milterHandleError(ctx uintptr, sfsistat *int8) {
	if Config.ClueGetter.Exit_On_Panic {
		return
	}
	r := recover()
	if r == nil {
		return
	}

	Log.Error("Panic ocurred while handling milter communication. Recovering. Error: %s", r)
	StatsCounters["MessagePanics"].increase(1)
	if Config.ClueGetter.Noop {
		return
	}

	s := milterGetSession(ctx, true, true)
	if s != nil && s.isWhitelisted() {
		return
	}

	m.SetReply(ctx, "421", "4.7.0", "An internal error ocurred")
	*sfsistat = m.Tempfail
	return
}

func milterLog(i ...interface{}) {
	Log.Debug(fmt.Sprintf("%s", i[:1]), i[1:]...)
}

func milterGetSession(ctx uintptr, keep bool, returnNil bool) *milterSession {
	var u uint64
	m.GetPriv(ctx, &u)
	if keep {
		m.SetPriv(ctx, u)
	}

	MilterDataIndex.mu.Lock()
	defer MilterDataIndex.mu.Unlock()

	out := MilterDataIndex.sessions[u]
	if out == nil && !returnNil {
		panic(fmt.Sprintf("Session %d could not be found in milterDataIndex", u))
	}

	return out
}
