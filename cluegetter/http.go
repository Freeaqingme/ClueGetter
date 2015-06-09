// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func httpStart(done <-chan struct{}) {
	listen_host := Config.ClueGetter.Http_Listen_Host
	listen_port := Config.ClueGetter.Http_Listen_Port

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

	messages := make([]*httpMessage, 0)
	rows, _ := Rdbms.Query(`
		SELECT m.id, m.date, m.sender_local || '@' || m.sender_domain sender, m.rcpt_count, m.verdict,
			GROUP_CONCAT(distinct IF(r.domain = '', r.local, (r.local || '@' || r.domain))) recipients
			FROM message m
				LEFT JOIN message_recipient mr on mr.message = m.id
				LEFT JOIN recipient r ON r.id = mr.recipient
			WHERE (m.sender_local = ? AND m.sender_domain = ?)
				OR (r.local = ? AND r.domain = ?)
			GROUP BY m.id ORDER BY date DESC LIMIT 0,250
	`, local, domain, local, domain)
	defer rows.Close()
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

	cwd, _ := os.Getwd()
	tpl := template.Must(template.ParseFiles(
		cwd+"/htmlTemplates/messageSearchEmail.html", cwd+"/htmlTemplates/skeleton.html"))

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

	cwd, _ := os.Getwd()
	tpl := template.Must(template.ParseFiles(cwd+"/htmlTemplates/message.html", cwd+"/htmlTemplates/skeleton.html"))

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

	cwd, _ := os.Getwd()
	tpl := template.Must(template.ParseFiles(cwd+"/htmlTemplates/index.html", cwd+"/htmlTemplates/skeleton.html"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "skeleton.html", foo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func httpStatsHandler(w http.ResponseWriter, r *http.Request) {
	foo := foo{Foo: "Blaat"}

	cwd, _ := os.Getwd()
	tpl := template.Must(template.ParseFiles(cwd+"/htmlTemplates/stats.html", cwd+"/htmlTemplates/skeleton.html"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "skeleton.html", foo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
