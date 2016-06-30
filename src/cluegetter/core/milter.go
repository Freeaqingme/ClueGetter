// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"cluegetter/address"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	m "github.com/Freeaqingme/gomilter"
	"github.com/pborman/uuid"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type milter struct {
	m.MilterRaw
}

type milterDataIndex struct {
	sessions map[[16]byte]*MilterSession
	mu       sync.RWMutex
}

func (di *milterDataIndex) addNewSession(sess *MilterSession) *MilterSession {
	di.mu.Lock()
	defer di.mu.Unlock()

	di.sessions[sess.getId()] = sess
	return sess
}

func (di *milterDataIndex) delete(s *MilterSession, lock bool) {
	s.DateDisconnect = time.Now()
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
		if now.Sub(v.DateConnect).Minutes() > 15 {
			Log.Debugf("Pruning session %d", k)
			di.delete(v, false)
		}
	}
}

var MilterDataIndex milterDataIndex

func milterStart() {
	MilterDataIndex = milterDataIndex{sessions: make(map[[16]byte]*MilterSession)}

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
	milter.Flags = m.ADDHDRS | m.ADDRCPT | m.CHGFROM | m.CHGBODY | m.CHGHDRS
	milter.Socket = Config.ClueGetter.Milter_Socket

	go func() {
		out := m.Run(milter)
		Log.Infof(fmt.Sprintf("Milter stopped. Exit code: %d", out))
		if out == -1 {
			Log.Fatalf("libmilter returned an error.")
		}
	}()

	go milterPrune()

	Log.Infof("Milter module started. Now listening on " + Config.ClueGetter.Milter_Socket)
}

func milterStop() {
	m.Stop()
}

func milterPrune() {
	ticker := time.NewTicker(60 * time.Second)

	for {
		select {
		case <-ticker.C:
			MilterDataIndex.prune()
		}
	}
}

/**
 * @See: https://www.percona.com/blog/2014/12/19/store-uuid-optimized-way/
 */
func milterGetNewSessionId() [16]byte {
	uuid := strings.Replace(uuid.NewUUID().String(), "-", "", -1)
	uuidSwapped := uuid[14:19] + uuid[9:14] + uuid[0:9] + uuid[19:24] + uuid[24:]

	uuidBytes, _ := hex.DecodeString(uuidSwapped)
	res := [16]byte{}
	copy(res[:], uuidBytes[:])
	return res
}

func (milter *milter) Connect(ctx uintptr, hostname string, ip net.IP) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	sess := &MilterSession{
		id:          milterGetNewSessionId(),
		DateConnect: time.Now(),
		Instance:    instance,
		config:      Config.sessionConfig(),
	}
	sess.Hostname = hostname
	sess.Ip = ip.String()
	sess.MtaHostName = m.GetSymVal(ctx, "j")
	sess.MtaDaemonName = m.GetSymVal(ctx, "{daemon_name}")

	if reverse, _ := net.LookupAddr(ip.String()); len(reverse) != 0 {
		sess.ReverseDns = reverse[0]
	}

	MilterDataIndex.addNewSession(sess)
	sessId := sess.getId()
	res := m.SetPriv(ctx, sessId)
	if res != 0 {
		panic(fmt.Sprintf("Session could not be stored in milterDataIndex"))
	}

	StatsCounters["MilterCallbackConnect"].increase(1)
	Log.Debugf("%s Milter.Connect() called: ip = %s, hostname = %s", sess.milterGetDisplayId(), ip, sess.ReverseDns)

	return m.Continue
}

func (milter *milter) Helo(ctx uintptr, helo string) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	sess := milterGetSession(ctx, true, true)
	StatsCounters["MilterCallbackHelo"].increase(1)
	Log.Debugf("%s Milter.Helo() called: helo = %s", sess.milterGetDisplayId(), helo)

	sess.Helo = helo
	sess.CertIssuer = m.GetSymVal(ctx, "{cert_issuer}")
	sess.CertSubject = m.GetSymVal(ctx, "{cert_subject}")
	sess.TlsVersion = m.GetSymVal(ctx, "{tls_version}")

	cipherBits, _ := strconv.Atoi(m.GetSymVal(ctx, "{cipher_bits}"))
	sess.CipherBits = uint32(cipherBits)
	sess.Cipher = m.GetSymVal(ctx, "{cipher}")

	return
}

func (milter *milter) EnvFrom(ctx uintptr, from []string) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	d := milterGetSession(ctx, true, false)
	msg := d.getNewMessage()

	StatsCounters["MilterCallbackEnvFrom"].increase(1)
	Log.Debugf("%s Milter.EnvFrom() called: from = %s", d.milterGetDisplayId(), from[0])

	if len(from) == 0 {
		StatsCounters["MilterProtocolErrors"].increase(1)
		Log.Critical("%s Milter.EnvFrom() callback received %d elements", d.milterGetDisplayId(), len(from))
		panic(fmt.Sprint("%s Milter.EnvFrom() callback received %d elements", d.milterGetDisplayId(), len(from)))
	}
	msg.From = address.FromString(strings.ToLower(strings.Trim(from[0], "<>")))
	return
}

func (milter *milter) EnvRcpt(ctx uintptr, rcpt []string) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	d := milterGetSession(ctx, true, false)
	msg := d.getLastMessage()
	Log.Debugf("%s Milter.EnvRcpt() called: rcpt = %s", d.milterGetDisplayId(), fmt.Sprint(rcpt))

	address := address.FromString(strings.ToLower(strings.Trim(rcpt[0], "<>")))
	verdict, msgOut := messageAcceptRecipient(address)

	switch {
	case verdict == MessageTempFail:
		Log.Debugf("%s Milter.EnvRcpt() status=TempFail rcpt=%s msg=%s", d.milterGetDisplayId(), address.String(), msgOut)
		return m.Tempfail
	case verdict == MessageReject:
		Log.Debugf("%s Milter.EnvRcpt() status=Reject rcpt=%s msg=%s", d.milterGetDisplayId(), address.String(), msgOut)
		return m.Reject
	case verdict == MessageError:
		Log.Errorf("%s Milter.EnvRcpt() status=Error rcpt=%s msg=%s", d.milterGetDisplayId(), address.String(), msgOut)
		return m.Tempfail
	}

	msg.Rcpt = append(msg.Rcpt, address)

	return
}

func (milter *milter) Header(ctx uintptr, headerf, headerv string) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	sess := milterGetSession(ctx, true, false)
	msg := sess.getLastMessage()

	msg.Headers = append(msg.Headers, MessageHeader{
		Key:       headerf,
		Value:     headerv,
		milterIdx: len(msg.GetHeader(headerf, false)) + 1},
	)

	StatsCounters["MilterCallbackHeader"].increase(1)
	Log.Debugf("%s Milter.Header() called: header %s = %s", sess.milterGetDisplayId(), headerf, headerv)
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

	StatsCounters["MilterCallbackEoh"].increase(1)
	Log.Debugf("%s milter.Eoh() was called", sess.milterGetDisplayId())
	return
}

func (milter *milter) Body(ctx uintptr, body []byte) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	s := milterGetSession(ctx, true, false)
	msg := s.getLastMessage()
	msg.Body = append(msg.Body, body...)

	StatsCounters["MilterCallbackBody"].increase(1)
	Log.Debugf("%s milter.Body() was called. Length of body: %d", s.milterGetDisplayId(), len(body))
	return
}

func (milter *milter) Eom(ctx uintptr) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	s := milterGetSession(ctx, true, false)
	StatsCounters["MilterCallbackEom"].increase(1)
	Log.Debugf("%s milter.Eom() was called", s.milterGetDisplayId())

	msg := s.getLastMessage()
	msg.Date = time.Now()
	msg.BodySize = len(msg.Body)
	msg.BodyHash = fmt.Sprintf("%x", md5.Sum(msg.Body))

	verdict, msgOut, results := messageGetVerdict(msg)
	headersAdd, headersDelete := messageGetMutableHeaders(s.getLastMessage(), results)
	for _, hdr := range headersDelete {
		var delete string // Must be null to delete
		m.ChgHeader(ctx, hdr.getKey(), hdr.milterIdx, delete)
		hdr.deleted = true

		for _, allHdr := range s.getLastMessage().GetHeader(hdr.getKey(), false) {
			if allHdr.milterIdx > hdr.milterIdx {
				allHdr.milterIdx = allHdr.milterIdx - 1
			}
		}
	}

	for _, hdr := range headersAdd {
		m.AddHeader(ctx, hdr.getKey(), hdr.getValue())
	}

	if s.isWhitelisted() {
		verdict = MessagePermit
		msgOut = "Whitelisted"
	}

	switch {
	case verdict == MessagePermit:
		Log.Infof("Message Permit: sess=%s message=%s %s", s.milterGetDisplayId(), s.getLastMessage().QueueId, msgOut)
		return
	case verdict == MessageTempFail:
		m.SetReply(ctx, "421", "4.7.0", fmt.Sprintf("%s (%s)", msgOut, s.getLastMessage().QueueId))
		Log.Infof("Message TempFail: sess=%s message=%s msg: %s", s.milterGetDisplayId(), s.getLastMessage().QueueId, msgOut)
		if Config.ClueGetter.Noop {
			return
		}
		return m.Tempfail
	case verdict == MessageReject:
		m.SetReply(ctx, "550", "5.7.1", fmt.Sprintf("%s (%s)", msgOut, s.getLastMessage().QueueId))
		Log.Infof("Message Reject: sess=%s message=%s msg: %s", s.milterGetDisplayId(), s.getLastMessage().QueueId, msgOut)
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
	Log.Debugf("milter.Abort() was called")
	milterGetSession(ctx, true, true)

	return
}

func (milter *milter) Close(ctx uintptr) (sfsistat int8) {
	defer milterHandleError(ctx, &sfsistat)

	StatsCounters["MilterCallbackClose"].increase(1)
	s := milterGetSession(ctx, false, true)
	if s == nil {
		Log.Debugf("%d milter.Close() was called. No context supplied")
		return
	}
	Log.Debugf("%s milter.Close() was called", s.milterGetDisplayId())

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

	Log.Errorf("Panic ocurred while handling milter communication. Recovering. Error: %s", r)
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

func MilterChangeFrom(sess *MilterSession, from string) {
	m.ChgFrom(sess.milterCtx, from, "")
}

func MilterAddRcpt(sess *MilterSession, rcpt string) int {
	return m.AddRcpt(sess.milterCtx, rcpt)
}

func MilterDelRcpt(sess *MilterSession, rcpt string) int {
	return m.DelRcpt(sess.milterCtx, rcpt)
}

func milterGetSession(ctx uintptr, keep bool, returnNil bool) *MilterSession {
	var u [16]byte
	res := m.GetPriv(ctx, &u)
	if res != 0 {
		// We purposefully do not act on errors. For some reason, the FreeBSD build always
		// returns an error. Also, in practice it never fails. Famous last words...
		//  panic("Could not get data from libmilter")
	}
	if keep {
		res := m.SetPriv(ctx, u)
		if res != 0 {
			panic(fmt.Sprintf("Session %d could not be stored in milterDataIndex", u))
		}
	}

	MilterDataIndex.mu.Lock()
	defer MilterDataIndex.mu.Unlock()

	out := MilterDataIndex.sessions[u]
	if out == nil && !returnNil {
		panic(fmt.Sprintf("Session %d could not be found in milterDataIndex", u))
	}

	if out != nil {
		out.milterCtx = ctx
	}
	return out
}

func (sess *MilterSession) milterGetDisplayId() string {
	id := sess.getId()
	return hex.EncodeToString(id[:])
}
