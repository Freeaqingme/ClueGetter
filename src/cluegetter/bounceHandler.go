// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"bufio"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"os"
	"strings"
	"time"
)

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

var BounceHandlerSaveBounceStmt = *new(*sql.Stmt)
var BounceHandlerSaveBounceReportStmt = *new(*sql.Stmt)

func init() {
	enable := func() bool { return Config.BounceHandler.Enabled }
	init := bounceHandlerStart
	stop := bounceHandlerStop
	handleIpc := bounceHandlerHandleIpc

	ModuleRegister(&module{
		name:   "bouncehandler",
		enable: &enable,
		init:   &init,
		stop:   &stop,
		ipc: map[string]func(string){
			"bouncehandler!submit": handleIpc,
		},
	})

	submitCli := bounceHandlerSubmitCli
	subAppRegister(&subApp{
		name:     "bouncehandler",
		handover: &submitCli,
	})
}

func bounceHandlerStart() {
	bounceHandlerPrepStmt()
	go bounceHandlerListen()
}

func bounceHandlerStop() {
	Log.Info("BounceHandler module stopped successfully")
}

// Submit a new report to the bounce handler through the CLI.
func bounceHandlerSubmitCli() {
	if len(os.Args) < 2 || os.Args[1] != "submit" {
		Log.Fatal("Missing argument for 'bouncehandler'. Must be one of: submit")
	}

	reader := bufio.NewReader(os.Stdin)
	body := make([]byte, 0)
	for {
		buf := make([]byte, 512)
		nr, err := reader.Read(buf)
		if err != nil {
			break
		}
		body = append(body, buf[:nr]...)
	}

	bodyB64 := base64.StdEncoding.EncodeToString(body)
	daemonIpcSend("bouncehandler!submit", bodyB64)
}

func bounceHandlerPrepStmt() {
	stmt, err := Rdbms.Prepare(
		"INSERT INTO bounce (cluegetter_instance, date, mta, queueId, messageId, sender) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		Log.Fatal(err)
	}
	BounceHandlerSaveBounceStmt = stmt

	stmt, err = Rdbms.Prepare(`
		INSERT INTO bounce_report (bounce, status, orig_rcpt, final_rcpt, remote_mta, diag_code)
			VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		Log.Fatal(err)
	}
	BounceHandlerSaveBounceReportStmt = stmt
}

func bounceHandlerListen() {
	listenStr := Config.BounceHandler.Listen_Host + ":" + Config.BounceHandler.Listen_Port
	l, err := net.Listen("tcp", listenStr)
	if err != nil {
		Log.Fatal(fmt.Sprintf("Cannot bind on tcp/%s: %s", listenStr, err.Error()))
	}

	defer l.Close()
	Log.Info("Now listening on tcp/%s", listenStr)

	for {
		conn, err := l.Accept()
		if err != nil {
			Log.Error("Could not accept new connection: ", err.Error())
			continue
		}
		go bounceHandlerHandleConn(conn)
	}
}

func bounceHandlerHandleConn(conn net.Conn) {
	defer cluegetterRecover("bounceHandlerParseReport")
	defer conn.Close()

	Log.Debug("Handling new connection from %s", conn.RemoteAddr())
	body := make([]byte, 0)
	for {
		buf := make([]byte, 512)
		nr, err := conn.Read(buf)
		if err != nil {
			break
		}

		body = append(body, buf[:nr]...)
	}

	bounceHandlerParseReport(body, conn.RemoteAddr().String())
}

func bounceHandlerHandleIpc(bodyB64 string) {
	Log.Debug("Received new report through IPC")

	body, err := base64.StdEncoding.DecodeString(bodyB64)
	if err != nil {
		Log.Error("Could not base64 decode report received over IPC: %s", err.Error())
		return
	}

	bounceHandlerParseReport(body, "IPC")
}

func bounceHandlerParseReport(body []byte, remoteAddr string) {
	bounceHandlerPersistRawCopy(body)

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
		instance:    instance,
		date:        &date,
		mta:         bounceReasons.Get("Reporting-MTA"),
		queueId:     bounceReasons.Get("X-Postfix-Queue-Id"),
		sender:      bounceReasons.Get("X-Postfix-Sender"),
		messageId:   bounceHandlerGetMessageId(msgHdrs, bounceReasons.Get("X-Postfix-Queue-Id")),
		rcptReports: rcptReports,
	}

	bounceHandlerSaveBounce(bounce)
	if bayesReason != "" {
		go bayesReportMessageId(true, bounce.messageId, bounce.mta, "__MTA", bayesReason)

		queueId := bounceHandlerGetHeaderFromBytes(deliveryReports, "X-Postfix-Queue-ID")
		mailQueueDeleteItems([]string{queueId})
	}
	Log.Info("Successfully saved %d reports from %s", len(rcptReports), remoteAddr)
}

func bounceHandlerGetMessageId(msg []byte, queueId string) string {
	r := strings.NewReader(string(msg) + "\r\n\r\n")
	m, err := mail.ReadMessage(r)
	if err != nil {
		return ""
	}

	if id := m.Header.Get("Message-Id"); id != "" {
		return id
	}
	return messageGenerateMessageId(queueId, "")
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

func bounceHandlerSaveBounce(bounce *bounceHandlerBounce) {
	StatsCounters["RdbmsQueries"].increase(1)
	res, err := BounceHandlerSaveBounceStmt.Exec(
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
		StatsCounters["RdbmsQueries"].increase(1)
		_, err := BounceHandlerSaveBounceReportStmt.Exec(
			id, report.status, report.origRecipient, report.finalRecipient, report.remoteMta, report.diagCode,
		)
		if err != nil {
			panic("Could not execute BounceHandlerSaveBounceReportStmt. Error: " + err.Error())
		}
	}
}

func bounceHandlerPersistRawCopy(body []byte) {
	defer cluegetterRecover("bounceHandlerPersistRawCopy")

	if Config.BounceHandler.Dump_Dir == "" {
		return
	}

	f, err := ioutil.TempFile(Config.BounceHandler.Dump_Dir, "cluegetter-deliveryreport-")
	if err != nil {
		Log.Error("Could not open file for delivery report: %s", err.Error())
		return
	}

	defer f.Close()
	count, err := f.Write(body)
	if err != nil {
		Log.Error("Wrote %d bytes to %s, then got error: %s", count, f.Name(), err.Error())
		return
	}

	Log.Debug("Wrote %d bytes to %s", count, f.Name())
}

// We only want to add emails to our bayes corpus if the remote
// side deems there's something wrong with the contents. Not if
// there's e.g. something wrong with the recipient's mailbox.
func bounceHandlerShouldReportBayes(diagCode string) bool {
	matches := []string{
		"spam",
		"unsolicited",
		"contained unsafe content",

		//		"x-unix", // DEBUG
	}

	for _, match := range matches {
		if strings.Contains(diagCode, match) {
			return true
		}
	}

	return false
}
