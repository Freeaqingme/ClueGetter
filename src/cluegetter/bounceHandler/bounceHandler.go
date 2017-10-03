// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package bounceHandler

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strings"
	"time"

	"cluegetter/core"
)

const ModuleName = "bouncehandler"

type module struct {
	*core.BaseModule

	bounceHandlerSaveBounceStmt       *sql.Stmt
	bounceHandlerSaveBounceReportStmt *sql.Stmt
}

type bounceHandlerBounce struct {
	id          uint64
	instance    uint
	date        *time.Time
	mta         string
	queueId     string
	messageId   string
	sender      string //Todo: Split this out later to local/remote part but want to inventory formats first
	rcptReports []*bounceHandlerRcptReport
}

type bounceHandlerRcptReport struct {
	status         string
	origRecipient  string //Todo: Split this out later to local/remote part but want to inventory formats first
	finalRecipient string //Todo: Split this out later to local/remote part but want to inventory formats first
	remoteMta      string
	diagCode       string
}

type bayesModule interface {
	ReportMessageId(spam bool, messageId, host, reporter, reason string)
}

func init() {
	core.ModuleRegister(&module{
		BaseModule: core.NewBaseModule(nil),
	})
}

func (m *module) Name() string {
	return ModuleName
}

func (m *module) config() core.ConfigBounceHandler {
	return m.Config().BounceHandler
}

func (m *module) Enable() bool {
	return m.config().Enabled
}

func (m *module) Init() error {
	m.prepStmt()

	go m.listen()

	return nil
}

func (m *module) Ipc() map[string]func(string) {
	return map[string]func(string){
		"bouncehandler!submit": m.handleIpc,
	}
}

func (m *module) prepStmt() {
	stmt, err := m.Rdbms().Prepare(
		"INSERT INTO bounce (cluegetter_instance, date, mta, queueId, messageId, sender) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		m.Log().Fatalf("%s", err)
	}
	m.bounceHandlerSaveBounceStmt = stmt

	stmt, err = m.Rdbms().Prepare(`
		INSERT INTO bounce_report (bounce, status, orig_rcpt, final_rcpt, remote_mta, diag_code)
			VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		m.Log().Fatalf("%s", err)
	}
	m.bounceHandlerSaveBounceReportStmt = stmt
}

func (m *module) listen() {
	listenStr := m.config().Listen_Host + ":" + m.config().Listen_Port
	l, err := net.Listen("tcp", listenStr)
	if err != nil {
		m.Log().Fatalf(fmt.Sprintf("Cannot bind on tcp/%s: %s", listenStr, err.Error()))
	}

	defer l.Close()
	m.Log().Infof("Now listening on tcp/%s", listenStr)

	for {
		conn, err := l.Accept()
		if err != nil {
			m.Log().Errorf("Could not accept new connection: ", err.Error())
			continue
		}
		go m.handleConn(conn)
	}
}

func (m *module) handleConn(conn net.Conn) {
	defer core.CluegetterRecover("bounceHandler.ParseReport")
	defer conn.Close()

	m.Log().Debugf("Handling new connection from %s", conn.RemoteAddr())
	body := make([]byte, 0)
	for {
		buf := make([]byte, 512)
		nr, err := conn.Read(buf)
		if err != nil {
			break
		}

		body = append(body, buf[:nr]...)
	}

	m.parseReport(body, conn.RemoteAddr().String())
}

func (m *module) handleIpc(bodyB64 string) {
	m.Log().Debugf("Received new report through IPC")

	body, err := base64.StdEncoding.DecodeString(bodyB64)
	if err != nil {
		m.Log().Errorf("Could not base64 decode report received over IPC: %s", err.Error())
		return
	}

	m.parseReport(body, "IPC")
}

func (m *module) parseReport(body []byte, remoteAddr string) {
	m.persistRawCopy(body)

	hdrs, deliveryReports, _, msgHdrs := bounceHandlerParseReportMime(string(body))
	bounceReasons, rcptReportsHdrs := bounceHandlerGetBounceReasons(deliveryReports)

	rcptReports := make([]*bounceHandlerRcptReport, len(rcptReportsHdrs))
	bayesReason := ""
	for i, rcptReport := range rcptReportsHdrs {
		rcptReports[i] = &bounceHandlerRcptReport{
			rcptReport.Get("Status"),
			rcptReport.Get("Original-Recipient"),
			rcptReport.Get("Final-Recipient"),
			rcptReport.Get("Remote-Mta"),
			rcptReport.Get("Diagnostic-Code"),
		}

		if bounceHandlerShouldReportBayes(rcptReports[i].diagCode) {
			bayesReason = rcptReports[i].diagCode
		}
	}

	date, _ := hdrs.Date()
	bounce := &bounceHandlerBounce{
		instance:    m.Instance(),
		date:        &date,
		mta:         bounceReasons.Get("Reporting-MTA"),
		queueId:     bounceReasons.Get("X-Postfix-Queue-Id"),
		sender:      bounceReasons.Get("X-Postfix-Sender"),
		messageId:   m.getMessageId(msgHdrs, bounceReasons.Get("X-Postfix-Queue-Id")),
		rcptReports: rcptReports,
	}

	m.saveBounce(bounce)
	if bayesReason != "" {
		bayes := (*m.Module("bayes", "")).(bayesModule)
		if bayes != nil {
			go bayes.ReportMessageId(true, bounce.messageId, bounce.mta, "__MTA", bayesReason)
		}

		queueId := bounceHandlerGetHeaderFromBytes(deliveryReports, "X-Postfix-Queue-ID")
		core.MailQueueDeleteItems([]string{queueId}) // TODO: Once this becomes a module...
	}
	m.Log().Infof("Successfully saved %d reports from %s", len(rcptReports), remoteAddr)
}

func (m *module) getMessageId(msgBytes []byte, queueId string) string {
	r := strings.NewReader(string(msgBytes) + "\r\n\r\n")
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return ""
	}

	if id := msg.Header.Get("Message-Id"); id != "" {
		return id
	}
	return core.MessageGenerateMessageId(queueId, "")
}

func bounceHandlerGetHeaderFromBytes(msg []byte, header string) string {
	r := strings.NewReader(string(msg) + "\r\n\r\n")
	m, err := mail.ReadMessage(r)
	if err != nil {
		return ""
	}

	return m.Header.Get(header)
}

func bounceHandlerGetBounceReasons(notification []byte) (mail.Header, []mail.Header) {
	r := strings.NewReader(strings.TrimLeft(string(notification)+"\r\n\r\n", "\n\r "))
	m, err := mail.ReadMessage(r)
	if err != nil {
		panic(err)
	}

	body, err := ioutil.ReadAll(m.Body)
	if err != nil {
		panic(err)
	}

	rcptReports := make([]mail.Header, 0)
	for _, subMailStr := range strings.Split(string(body), "\n\n") {
		r = strings.NewReader(subMailStr + "\r\n\r\n")
		subMail, err := mail.ReadMessage(r)
		if err != nil {
			panic(err)
		}
		rcptReports = append(rcptReports, subMail.Header)
	}

	return m.Header, rcptReports
}

func bounceHandlerParseReportMime(msg string) (bounceHdrs mail.Header, deliveryReport, notification, msgHdrs []byte) {
	r := strings.NewReader(msg)
	m, err := mail.ReadMessage(r)
	if err != nil {
		panic(err)
	}

	mediaType, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil {
		panic(err)
	}

	if !strings.HasPrefix(mediaType, "multipart/") {
		panic("Received message is not of type multipart")
	}

	bounceHdrs = m.Header
	mr := multipart.NewReader(m.Body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			p.Close()
			panic(err)
		}

		slurp, err := ioutil.ReadAll(p)
		if err != nil {
			p.Close()
			panic(err)
		}

		if p.Header.Get("Content-Description") == "Notification" {
			notification = slurp
		}
		if p.Header.Get("Content-Description") == "Delivery report" {
			deliveryReport = slurp
		}
		if p.Header.Get("Content-Description") == "Undelivered Message Headers" {
			msgHdrs = slurp
		}

		p.Close()
	}
}

func (m *module) saveBounce(bounce *bounceHandlerBounce) {
	res, err := m.bounceHandlerSaveBounceStmt.Exec(
		bounce.instance, bounce.date, bounce.mta, bounce.queueId, bounce.messageId, bounce.sender,
	)
	if err != nil {
		panic("Could not execute BounceHandlerSaveBounceStmt. Error: " + err.Error())
	}

	id, err := res.LastInsertId()
	if err != nil {
		panic("Could not get last insert id: " + err.Error())
	}

	for _, report := range bounce.rcptReports {
		_, err := m.bounceHandlerSaveBounceReportStmt.Exec(
			id, report.status, report.origRecipient, report.finalRecipient, report.remoteMta, report.diagCode,
		)
		if err != nil {
			panic("Could not execute BounceHandlerSaveBounceReportStmt. Error: " + err.Error())
		}
	}
}

func (m *module) persistRawCopy(body []byte) {
	defer core.CluegetterRecover("bounceHandlerPersistRawCopy")

	if m.config().Dump_Dir == "" {
		return
	}

	f, err := ioutil.TempFile(m.config().Dump_Dir, "cluegetter-deliveryreport-")
	if err != nil {
		m.Log().Errorf("Could not open file for delivery report: %s", err.Error())
		return
	}

	defer f.Close()
	count, err := f.Write(body)
	if err != nil {
		m.Log().Errorf("Wrote %d bytes to %s, then got error: %s", count, f.Name(), err.Error())
		return
	}

	m.Log().Debugf("Wrote %d bytes to %s", count, f.Name())
}

// We only want to add emails to our bayes corpus if the remote
// side deems there's something wrong with the contents. Not if
// there's e.g. something wrong with the recipient's mailbox.
func bounceHandlerShouldReportBayes(diagCode string) bool {
	matches := []string{
		"spam",
		"unsolicited",
		"contained unsafe content",

		// "x-unix", // DEBUG
	}

	for _, match := range matches {
		if strings.Contains(diagCode, match) {
			return true
		}
	}

	return false
}
