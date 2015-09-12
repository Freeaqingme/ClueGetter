// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"cluegetter/assets"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"
	"time"
)

func httpStart(done <-chan struct{}) {
	if !Config.Http.Enabled {
		Log.Info("HTTP module has not been enabled. Skipping...")
		return
	}
	listen_host := Config.Http.Listen_Host
	listen_port := Config.Http.Listen_Port

	laddr, err := net.ResolveTCPAddr("tcp", listen_host+":"+listen_port)
	if nil != err {
		Log.Fatal(err)
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if nil != err {
		Log.Fatal(err)
	}
	Log.Info("HTTP interface now listening on %s", listener.Addr())

	http.HandleFunc("/stats/", httpStatsHandler)
	http.HandleFunc("/message/", httpHandlerMessage)
	http.HandleFunc("/message/searchEmail/", httpHandlerMessageSearchEmail)
	http.HandleFunc("/message/searchClientAddress/", httpHandlerMessageSearchClientAddress)
	http.HandleFunc("/message/searchSaslUser/", httpHandleMessageSearchSaslUser)
	http.HandleFunc("/", httpIndexHandler)

	go http.Serve(listener, nil)

	go func() {
		<-done
		listener.Close()
		Log.Info("HTTP Listener closed")
	}()
}

type foo struct {
	Foo string
}

type httpMessage struct {
	Recipients   []*httpMessageRecipient
	Headers      []*httpMessageHeader
	CheckResults []*httpMessageCheckResult

	Ip           string
	SaslUsername string

	Id            string
	SessionId     int
	Date          *time.Time
	Sender        string
	RcptCount     int
	Verdict       string
	VerdictMsg    string
	RejectScore   float64
	TempfailScore float64
}

type httpMessageRecipient struct {
	Id     int
	Local  string
	Domain string
	Email  string
}

type httpMessageHeader struct {
	Name string
	Body string
}

type httpMessageCheckResult struct {
	Module       string
	Verdict      string
	Score        float64
	Determinants string
}

func httpHandlerMessageSearchEmail(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Path[len("/message/searchEmail/"):]
	var local, domain string
	if strings.Index(address, "@") != -1 {
		local = strings.Split(address, "@")[0]
		domain = strings.Split(address, "@")[1]
	} else {
		domain = address
	}

	rows, err := Rdbms.Query(`
		SELECT m.id, m.date, m.sender_local || '@' || m.sender_domain sender, m.rcpt_count, m.verdict,
			GROUP_CONCAT(distinct IF(r.domain = '', r.local, (r.local || '@' || r.domain))) recipients
			FROM message m
				LEFT JOIN message_recipient mr on mr.message = m.id
				LEFT JOIN recipient r ON r.id = mr.recipient
			WHERE (m.sender_domain = ? AND (m.sender_local = ? OR ? = ''))
				OR (r.domain = ? AND (r.local = ? OR ? = ''))
			GROUP BY m.id ORDER BY date DESC LIMIT 0,250
	`, domain, local, local, domain, local, local)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	httpProcessSearchResultRows(w, r, rows)
}

func httpHandlerMessageSearchClientAddress(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Path[len("/message/searchClientAddress/"):]

	rows, err := Rdbms.Query(`
		SELECT m.id, m.date, m.sender_local || '@' || m.sender_domain sender, m.rcpt_count, m.verdict,
			GROUP_CONCAT(distinct IF(r.domain = '', r.local, (r.local || '@' || r.domain))) recipients
			FROM message m
				LEFT JOIN message_recipient mr on mr.message = m.id
				LEFT JOIN recipient r ON r.id = mr.recipient
				LEFT JOIN session s ON m.session = s.id
			WHERE s.ip = ?
			GROUP BY m.id ORDER BY date DESC LIMIT 0,250
	`, net.ParseIP(address).String())
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	httpProcessSearchResultRows(w, r, rows)
}

func httpHandleMessageSearchSaslUser(w http.ResponseWriter, r *http.Request) {
	saslUser := r.URL.Path[len("/message/searchSaslUser/"):]

	rows, err := Rdbms.Query(`
		SELECT m.id, m.date, m.sender_local || '@' || m.sender_domain sender, m.rcpt_count, m.verdict,
			GROUP_CONCAT(distinct IF(r.domain = '', r.local, (r.local || '@' || r.domain))) recipients
			FROM message m
				LEFT JOIN message_recipient mr on mr.message = m.id
				LEFT JOIN recipient r ON r.id = mr.recipient
				LEFT JOIN session s ON m.session = s.id
			WHERE s.sasl_username = ?
			GROUP BY m.id ORDER BY date DESC LIMIT 0,250
	`, saslUser)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	httpProcessSearchResultRows(w, r, rows)
}

func httpProcessSearchResultRows(w http.ResponseWriter, r *http.Request, rows *sql.Rows) {
	messages := make([]*httpMessage, 0)
	for rows.Next() {
		message := &httpMessage{Recipients: make([]*httpMessageRecipient, 0)}
		var rcptsStr string
		rows.Scan(&message.Id, &message.Date, &message.Sender, &message.RcptCount,
			&message.Verdict, &rcptsStr)
		for _, rcpt := range strings.Split(rcptsStr, ",") {
			message.Recipients = append(message.Recipients, &httpMessageRecipient{Email: rcpt})
		}
		messages = append(messages, message)
	}

	if r.FormValue("json") == "1" {
		httpReturnJson(w, messages)
		return
	}

	tplMsgSearchEmail, _ := assets.Asset("htmlTemplates/messageSearchEmail.html")
	tplSkeleton, _ := assets.Asset("htmlTemplates/skeleton.html")
	tpl := template.New("skeleton.html")
	tpl.Parse(string(tplMsgSearchEmail))
	tpl.Parse(string(tplSkeleton))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "skeleton.html", messages); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func httpReturnJson(w http.ResponseWriter, obj interface{}) {
	jsonStr, _ := json.Marshal(obj)
	fmt.Fprintf(w, string(jsonStr))
}

func httpHandlerMessage(w http.ResponseWriter, r *http.Request) {
	queueId := r.URL.Path[len("/message/"):]
	row := Rdbms.QueryRow(
		"SELECT m.session, m.date, m.sender_local || '@' || m.sender_domain sender, "+
			"       m.rcpt_count, m.verdict, m.verdict_msg, "+
			"       m.rejectScore, m.tempfailScore, s.ip, s.sasl_username "+
			"FROM message m LEFT JOIN session s ON s.id = m.session WHERE m.id = ?", queueId)
	msg := &httpMessage{Recipients: make([]*httpMessageRecipient, 0)}
	row.Scan(&msg.SessionId, &msg.Date, &msg.Sender, &msg.RcptCount, &msg.Verdict,
		&msg.VerdictMsg, &msg.RejectScore, &msg.TempfailScore,
		&msg.Ip, &msg.SaslUsername)

	recipientRows, _ := Rdbms.Query(
		"SELECT r.id, r.local, r.domain FROM recipient r "+
			"LEFT JOIN message_recipient mr ON mr.recipient = r.id "+
			"LEFT JOIN message m ON m.id = mr.message WHERE message = ?", queueId)
	defer recipientRows.Close()
	for recipientRows.Next() {
		recipient := &httpMessageRecipient{}
		recipientRows.Scan(&recipient.Id, &recipient.Local, &recipient.Domain)
		if recipient.Domain == "" {
			recipient.Email = recipient.Local
		} else {
			recipient.Email = recipient.Local + "@" + recipient.Domain
		}
		msg.Recipients = append(msg.Recipients, recipient)
	}

	headerRows, _ := Rdbms.Query("SELECT name, body FROM message_header WHERE message = ?", queueId)
	defer headerRows.Close()
	for headerRows.Next() {
		header := &httpMessageHeader{}
		headerRows.Scan(&header.Name, &header.Body)
		msg.Headers = append(msg.Headers, header)
	}

	checkResultRows, _ := Rdbms.Query(
		"SELECT module, verdict, score, determinants FROM message_result WHERE message = ?", queueId)
	defer checkResultRows.Close()
	for checkResultRows.Next() {
		checkResult := &httpMessageCheckResult{}
		checkResultRows.Scan(&checkResult.Module, &checkResult.Verdict,
			&checkResult.Score, &checkResult.Determinants)
		msg.CheckResults = append(msg.CheckResults, checkResult)
	}

	if r.FormValue("json") == "1" {
		httpReturnJson(w, msg)
		return
	}

	tplSkeleton, _ := assets.Asset("htmlTemplates/skeleton.html")
	tplMsg, _ := assets.Asset("htmlTemplates/message.html")
	tpl := template.New("skeleton.html")
	tpl.Parse(string(tplMsg))
	tpl.Parse(string(tplSkeleton))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "skeleton.html", msg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func httpIndexHandler(w http.ResponseWriter, r *http.Request) {
	foo := foo{Foo: "Blaat"}

	if r.FormValue("queueId") != "" {
		http.Redirect(w, r, "/message/"+r.FormValue("queueId"), http.StatusFound)
		return
	}

	if r.FormValue("mailAddress") != "" {
		http.Redirect(w, r, "/message/searchEmail/"+r.FormValue("mailAddress"), http.StatusFound)
		return
	}

	if r.FormValue("clientAddress") != "" {
		http.Redirect(w, r, "/message/searchClientAddress/"+r.FormValue("clientAddress"), http.StatusFound)
		return
	}

	if r.FormValue("saslUser") != "" {
		http.Redirect(w, r, "/message/searchSaslUser/"+r.FormValue("saslUser"), http.StatusFound)
		return
	}

	tplIndex, _ := assets.Asset("htmlTemplates/index.html")
	tplSkeleton, _ := assets.Asset("htmlTemplates/skeleton.html")
	tpl := template.New("skeleton.html")
	tpl.Parse(string(tplIndex))
	tpl.Parse(string(tplSkeleton))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "skeleton.html", foo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func httpStatsHandler(w http.ResponseWriter, r *http.Request) {
	foo := foo{Foo: "Blaat"}

	tplStats, _ := assets.Asset("htmlTemplates/stats.html")
	tplSkeleton, _ := assets.Asset("htmlTemplates/skeleton.html")
	tpl := template.New("skeleton.html")
	tpl.Parse(string(tplStats))
	tpl.Parse(string(tplSkeleton))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "skeleton.html", foo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
