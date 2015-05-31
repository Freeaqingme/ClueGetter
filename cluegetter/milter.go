// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

// Todo: What if multiple messages are sent over single connection?
// Todo: Clean up sessions

import (
	"fmt"
	m "github.com/Freeaqingme/gomilter"
	"github.com/nu7hatch/gouuid"
	"sync"
	"time"
	"encoding/json"
)

type milter struct {
	m.MilterRaw
}

type milterDataIndex struct {
	sessions map[string]*milterData
	mu       sync.RWMutex
}

func (di *milterDataIndex) getNew() *milterData {
	u, err := uuid.NewV4()
	if err != nil {
		panic(fmt.Sprintf("Could not generate UUID. Lack of entropy? Error: %s"))
	}

	di.mu.Lock()
	defer di.mu.Unlock()

	data := &milterData{Id: u.String(), TimeStart: time.Now()}
	di.sessions[u.String()] = data
	return data
}

type milterData struct {
	Id           string
	TimeStart    time.Time
	QueueId      string
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
	From         string
	Rcpt         []string
	Header       []*milterDataHeader
	Body         string
}

type milterDataHeader struct {
	Key   string
	Value string
}

var MilterDataIndex milterDataIndex

func milterStart() {
	MilterDataIndex = milterDataIndex{sessions: make(map[string]*milterData)}

//	m.LoggerPrintln = milterLog
//	m.LoggerPrintf = Log.Debug

	StatsCounters["MilterCallbackConnect"] = &StatsCounter{}
	StatsCounters["MilterCallbackHelo"] = &StatsCounter{}
	StatsCounters["MilterCallbackEnvFrom"] = &StatsCounter{}
	StatsCounters["MilterCallbackEnvRcpt"] = &StatsCounter{}
	StatsCounters["MilterCallbackHeader"] = &StatsCounter{}
	StatsCounters["MilterCallbackEnvFromErrors"] = &StatsCounter{}

	milter := new(milter)
	milter.FilterName = "GlueGetter"
	milter.Debug = true
	milter.Flags = m.ADDHDRS | m.ADDRCPT | m.CHGFROM | m.CHGBODY
	milter.Socket = "inet:10033@127.0.0.1" // Todo: Should be configurable

	go func() {
		if m.Run(milter) == -1 {
			// Todo: May just want to retry?
			Log.Fatal("libmilter returned an error.")
		}
	}()

}

func (milter *milter) Connect(ctx uintptr, hostname, ip string) (sfsistat int8) {
	d := MilterDataIndex.getNew()
	d.Hostname = hostname
	d.Ip = ip
	m.SetPriv(ctx, d.Id)

	StatsCounters["MilterCallbackConnect"].increase(1)
	Log.Debug("%s Milter.Connect called: ip = %s, hostname = %s", d.Id, ip, hostname)

	return m.Continue
}

func (milter *milter) Helo(ctx uintptr, helo string) (sfsistat int8) {
	d := milterGetPriv(ctx, true)
	d.Helo = helo
	d.CertIssuer = m.GetSymVal(ctx, "{cert_issuer}")
	d.CertSubject = m.GetSymVal(ctx, "{cert_subject}")
	d.CipherBits = m.GetSymVal(ctx, "{cipher_bits}")
	d.Cipher = m.GetSymVal(ctx, "{cipher}")
	d.TlsVersion = m.GetSymVal(ctx, "{tls_version}")

	StatsCounters["MilterCallbackHelo"].increase(1)
	Log.Debug("%s Milter.Helo called: helo = %s", d.Id, helo)

	return
}

func (milter *milter) EnvFrom(ctx uintptr, from []string) (sfsistat int8) {
	d := milterGetPriv(ctx, true)

	StatsCounters["MilterCallbackEnvFrom"].increase(1)
	Log.Debug("%s Milter.EnvFrom called: from = %s", d.Id, from[0])

	if len(from) != 1 {
		StatsCounters["MilterCallbackEnvFromErrors"].increase(1)
		Log.Critical("%s Milter.EnvFrom callback received %d elements: %s", d.Id, len(from), fmt.Sprint(from))
	}
	d.From = from[0]
	return
}

func (milter *milter) EnvRcpt(ctx uintptr, rcpt []string) (sfsistat int8) {
	d := milterGetPriv(ctx, true)
	d.Rcpt = append(d.Rcpt, rcpt[0])

	StatsCounters["MilterCallbackEnvRcpt"].increase(1)
	Log.Debug("%s Milter.EnvRcpt called: rcpt = %s", d.Id, fmt.Sprint(rcpt))
	return
}

func (milter *milter) Header(ctx uintptr, headerf, headerv string) (sfsistat int8) {
	d := milterGetPriv(ctx, true)
	d.Header = append(d.Header, &milterDataHeader{headerf, headerv})

	StatsCounters["MilterCallbackHeader"].increase(1)
	Log.Debug("%s Milter.Header called: header %s = %s", d.Id, headerf, headerv)
	return
}

func (milter *milter) Eoh(ctx uintptr) (sfsistat int8) {
	d := milterGetPriv(ctx, true)
	d.QueueId = m.GetSymVal(ctx, "i")
	d.SaslUsername = m.GetSymVal(ctx, "{auth_authen}")
	d.SaslSender = m.GetSymVal(ctx, "{auth_author}")
	d.SaslMethod = m.GetSymVal(ctx, "{auth_type}")

	Log.Debug("%s milter.Eoh was called", d.Id)
	return
}

//Todo: Body can be called multiple times.
func (milter *milter) Body(ctx uintptr, body []byte) (sfsistat int8) {
	d := milterGetPriv(ctx, true)
	d.Body = string(body)

	Log.Debug("%s milter.Body was called", d.Id)
	return
}

func (milter *milter) Eom(ctx uintptr) (sfsistat int8) {
	d := milterGetPriv(ctx, false)
	Log.Debug("%s milter.Eom was called", d.Id)

//	fmt.Println(m.SetReply(ctx, "521", "5.7.1", "we dont like you"))
//	return m.Reject
	jsonStr, _ := json.Marshal(d)
	fmt.Println(string(jsonStr))
	return
}

func (milter *milter) Abort(ctx uintptr) (sfsistat int8) {
	_ := milterGetPriv(ctx, false)
	Log.Debug("milter.Abort was called")
	return
}

func (milter *milter) Close(ctx uintptr) (sfsistat int8) {
	_ := milterGetPriv(ctx, false)
	Log.Debug("milter.Close was called")
	return
}

func milterLog(i ...interface{}) {
	Log.Debug(fmt.Sprintf("%s", i[:1]), i[1:]...)
}

func milterGetPriv(ctx uintptr, keep bool) *milterData {
	var u string
	m.GetPriv(ctx, &u)
	if keep {
		m.SetPriv(ctx, u)
	}

	return MilterDataIndex.sessions[u]
}
