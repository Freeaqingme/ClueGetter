// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

/**
@TODO: Implement REDIS stuff?
*/

import (
	"database/sql"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strings"
	"time"
)

type bounceHandlerBounce struct {
	id          uint64
	instance    uint
	date        *time.Time
	mta         string
	queueId     string
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

	ModuleRegister(&module{
		name:   "bouncehandler",
		enable: &enable,
		init:   &init,
		stop:   &stop,
	})
}

func bounceHandlerStart() {
	bounceHandlerPrepStmt()
	go bounceHandlerListen()
}

func bounceHandlerStop() {
	Log.Info("BounceHandler module stopped successfully")
}

func bounceHandlerPrepStmt() {
	stmt, err := Rdbms.Prepare(
		"INSERT INTO bounce (cluegetter_instance, date, mta, queueId, sender) VALUES (?, ?, ?, ?, ?)")
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
		go bounceHandlerParseReport(conn)
	}
}

func bounceHandlerParseReport(conn net.Conn) {
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

	hdrs, deliveryReports, _, _ := bounceHandlerParseReportMime(string(body))
	bounceReasons, rcptReportsHdrs := bounceHandlerGetBounceReasons(deliveryReports)

	rcptReports := make([]*bounceHandlerRcptReport, len(rcptReportsHdrs))
	for i, rcptReport := range rcptReportsHdrs {
		rcptReports[i] = &bounceHandlerRcptReport{
			rcptReport.Get("Status"),
			rcptReport.Get("Original-Recipient"),
			rcptReport.Get("Final-Recipient"),
			rcptReport.Get("Remote-Mta"),
			rcptReport.Get("Diagnostic-Code"),
		}
	}

	date, _ := hdrs.Date()
	bounce := &bounceHandlerBounce{
		instance:    instance,
		date:        &date,
		mta:         bounceReasons.Get("Reporting-MTA"),
		queueId:     bounceReasons.Get("X-Postfix-Queue-Id"),
		sender:      bounceReasons.Get("X-Postfix-Sender"),
		rcptReports: rcptReports,
	}

	Log.Debug("Successfully parsed %d reports from %s", len(rcptReports), conn.RemoteAddr())
	bounceHandlerSaveBounce(bounce)
	Log.Info("Successfully saved %d reports from %s", len(rcptReports), conn.RemoteAddr())
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

func bounceHandlerParseReportMime(msg string) (bounceHdrs mail.Header, deliveryReport []byte, notification []byte, msgHdrs []byte) {
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
		bounce.instance, bounce.date, bounce.mta, bounce.queueId, bounce.sender,
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
