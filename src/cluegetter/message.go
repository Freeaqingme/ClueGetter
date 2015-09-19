// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	messagePermit = iota
	messageTempFail
	messageReject
)

type Session interface {
	getId() uint64
	getSaslUsername() string
	getSaslSender() string
	getSaslMethod() string
	getCertIssuer() string
	getCertSubject() string
	getCipherBits() string
	getCipher() string
	getTlsVersion() string
	getIp() string
	getHostname() string
	getHelo() string
	getMtaHostName() string
}

type Message interface {
	getSession() *Session
	getHeaders() []*MessageHeader

	getQueueId() string
	getFrom() string
	getRcptCount() int
	getRecipients() []string
	getBody() []string

	setInjectMessageId(string)
	getInjectMessageId() string
}

type MessageHeader interface {
	getKey() string
	getValue() string
}

type MessageCheckResult struct {
	module          string
	suggestedAction int
	message         string
	score           float64
	determinants    map[string]interface{}
	duration        time.Duration
}

var MessageInsertMsgStmt = *new(*sql.Stmt)
var MessageInsertMsgBodyStmt = *new(*sql.Stmt)
var MessageInsertRcptStmt = *new(*sql.Stmt)
var MessageInsertMsgRcptStmt = *new(*sql.Stmt)
var MessageInsertMsgHdrStmt = *new(*sql.Stmt)
var MessageSetVerdictStmt = *new(*sql.Stmt)
var MessageInsertModuleResultStmt = *new(*sql.Stmt)
var MessageInsertHeaders = make([]*milterMessageHeader, 0)
var MessagePruneBodyStmt = *new(*sql.Stmt)
var MessagePruneHeaderStmt = *new(*sql.Stmt)
var MessagePruneMessageResultStmt = *new(*sql.Stmt)
var MessagePruneMessageQuotaStmt = *new(*sql.Stmt)
var MessagePruneMessageStmt = *new(*sql.Stmt)
var MessagePruneMessageRecipient = *new(*sql.Stmt)
var MessagePruneRecipient = *new(*sql.Stmt)

func messageStart() {
	for _, hdrString := range Config.ClueGetter.Add_Header {
		if strings.Index(hdrString, ":") < 1 {
			Log.Fatal("Invalid header specified: ", hdrString)
		}

		header := &milterMessageHeader{
			strings.SplitN(hdrString, ":", 2)[0],
			strings.Trim(strings.SplitN(hdrString, ":", 2)[1], " "),
		}
		MessageInsertHeaders = append(MessageInsertHeaders, header)
	}

	if Config.ClueGetter.Archive_Retention_Message < Config.ClueGetter.Archive_Retention_Body ||
		Config.ClueGetter.Archive_Retention_Message < Config.ClueGetter.Archive_Retention_Header ||
		Config.ClueGetter.Archive_Retention_Message < Config.ClueGetter.Archive_Retention_Message_Result {
		Log.Fatal("Config Error: Message retention time should be at least as long as body and header retention time")
	}

	statsInitCounter("MessagePanics")
	statsInitCounter("MessageVerdictPermit")
	statsInitCounter("MessageVerdictTempfail")
	statsInitCounter("MessageVerdictReject")
	statsInitCounter("MessageVerdictRejectQuotas")
	statsInitCounter("MessageVerdictRejectSpamassassin")
	statsInitCounter("MessageVerdictRejectGreylisting")
	statsInitCounter("MessageVerdictTempfailQuotas")
	statsInitCounter("MessageVerdictTempfailSpamassassin")
	statsInitCounter("MessageVerdictTempfailGreylisting")

	stmt, err := Rdbms.Prepare(`INSERT INTO message (id, session, date, messageId, sender_local,
								sender_domain, rcpt_count) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertMsgStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT INTO message_body(message, sequence, body) VALUES(?, ?, ?)
								ON DUPLICATE KEY UPDATE message=LAST_INSERT_ID(message)`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertMsgBodyStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT INTO recipient(local, domain) VALUES(?, ?)
								ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertRcptStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT IGNORE INTO message_recipient(message, recipient, count) VALUES(?, ?,1)
								ON DUPLICATE KEY UPDATE count=count+1`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertMsgRcptStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT INTO message_header(message, name, body) VALUES(?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertMsgHdrStmt = stmt

	stmt, err = Rdbms.Prepare(`UPDATE message SET verdict=?, verdict_msg=?, rejectScore=?, tempfailScore=? WHERE id=?`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageSetVerdictStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT INTO message_result (message, module, verdict,
								score, duration, determinants) VALUES(?, ?, ?, ?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}
	MessageInsertModuleResultStmt = stmt

	MessagePruneBodyStmt, err = Rdbms.Prepare(`
		DELETE FROM message_body WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (DATE(?) - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessagePruneHeaderStmt, err = Rdbms.Prepare(`
		DELETE FROM message_header WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (DATE(?) - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessagePruneMessageResultStmt, err = Rdbms.Prepare(`
		DELETE FROM message_result WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (DATE(?) - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessagePruneMessageQuotaStmt, err = Rdbms.Prepare(`
		DELETE FROM quota_message WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (DATE(?) - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessagePruneMessageStmt, err = Rdbms.Prepare(`
		DELETE FROM message
		WHERE date < (DATE(?) - INTERVAL ? WEEK)
			AND session IN
				(SELECT id FROM session s WHERE s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessagePruneMessageRecipient, err = Rdbms.Prepare(`
		DELETE FROM message_recipient WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (DATE(?) - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessagePruneRecipient, err = Rdbms.Prepare(`
		DELETE FROM recipient WHERE NOT EXISTS
			(SELECT ?,?,? FROM message_recipient mr WHERE mr.recipient = recipient.id)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	go messagePrune()

	Log.Info("Message handler started successfully")
}

func messageStop() {
	MessageInsertMsgStmt.Close()
	Log.Info("Message handler stopped successfully")
}

func messageGetVerdict(msg Message) (verdict int, msgStr string, results [3][]*MessageCheckResult) {
	defer func() {
		if Config.ClueGetter.Exit_On_Panic {
			return
		}
		r := recover()
		if r == nil {
			return
		}
		Log.Error("Panic caught in messageGetVerdict(). Recovering. Error: %s", r)
		StatsCounters["MessagePanics"].increase(1)
		verdict = messageTempFail
		msgStr = "An internal error occurred."
		return
	}()

	messageSave(msg)

	results[messagePermit] = make([]*MessageCheckResult, 0)
	results[messageTempFail] = make([]*MessageCheckResult, 0)
	results[messageReject] = make([]*MessageCheckResult, 0)

	var totalScores [3]float64

	verdictValue := [3]string{"permit", "tempfail", "reject"}
	for result := range messageGetResults(msg) {
		results[result.suggestedAction] = append(results[result.suggestedAction], result)
		totalScores[result.suggestedAction] += result.score

		if Config.ClueGetter.Archive_Retention_Message_Result > 0 {
			determinants, _ := json.Marshal(result.determinants)
			StatsCounters["RdbmsQueries"].increase(1)
			_, err := MessageInsertModuleResultStmt.Exec(
				msg.getQueueId(), result.module, verdictValue[result.suggestedAction],
				result.score, result.duration.Seconds(), determinants)
			if err != nil {
				StatsCounters["RdbmsErrors"].increase(1)
				Log.Error(err.Error())
			}
		}
	}

	getDecidingResultWithMessage := func(results []*MessageCheckResult) *MessageCheckResult {
		out := results[0]
		maxScore := float64(0)
		for _, result := range results {
			if result.score > maxScore && result.message != "" {
				out = result
				maxScore = result.score
			}
		}
		return out
	}

	if totalScores[messageReject] >= Config.ClueGetter.Message_Reject_Score {
		determinant := getDecidingResultWithMessage(results[messageReject])
		StatsCounters["MessageVerdictReject"].increase(1)
		StatsCounters["MessageVerdictReject"+strings.Title(determinant.module)].increase(1)
		messageSaveVerdict(msg, messageReject, determinant.message, totalScores[messageReject], totalScores[messageTempFail])
		return messageReject, determinant.message, results
	}
	if (totalScores[messageTempFail] + totalScores[messageReject]) >= Config.ClueGetter.Message_Tempfail_Score {
		determinant := getDecidingResultWithMessage(results[messageTempFail])
		StatsCounters["MessageVerdictTempfail"].increase(1)
		StatsCounters["MessageVerdictTempfail"+strings.Title(determinant.module)].increase(1)
		messageSaveVerdict(msg, messageTempFail, determinant.message, totalScores[messageReject], totalScores[messageTempFail])
		return messageTempFail, determinant.message, results
	}

	StatsCounters["MessageVerdictPermit"].increase(1)
	messageSaveVerdict(msg, messagePermit, "", totalScores[messageReject], totalScores[messageTempFail])
	verdict = messagePermit
	msgStr = ""
	return
}

func messageSaveVerdict(msg Message, verdict int, verdictMsg string, rejectScore float64, tempfailScore float64) {
	verdictValue := [3]string{"permit", "tempfail", "reject"}

	StatsCounters["RdbmsQueries"].increase(1)
	_, err := MessageSetVerdictStmt.Exec(
		verdictValue[verdict],
		verdictMsg,
		rejectScore,
		tempfailScore,
		msg.getQueueId(),
	)

	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
	}
}

func messageGetResults(msg Message) chan *MessageCheckResult {
	var wg sync.WaitGroup
	out := make(chan *MessageCheckResult)

	modules := messageGetEnabledModules()
	for moduleName, moduleCallback := range modules {
		wg.Add(1)
		go func(moduleName string, moduleCallback func(Message) *MessageCheckResult) {
			defer wg.Done()
			t0 := time.Now()
			defer func() {
				if Config.ClueGetter.Exit_On_Panic {
					return
				}
				r := recover()
				if r == nil {
					return
				}
				Log.Error("Panic caught in %s. Recovering. Error: %s", moduleName, r)
				StatsCounters["MessagePanics"].increase(1)

				determinants := make(map[string]interface{})
				determinants["error"] = r

				out <- &MessageCheckResult{
					module:          moduleName,
					suggestedAction: messageTempFail,
					message:         "An internal error ocurred",
					score:           500,
					determinants:    determinants,
					duration:        time.Now().Sub(t0),
				}
			}()

			res := moduleCallback(msg)
			res.duration = time.Now().Sub(t0)
			out <- res
		}(moduleName, moduleCallback)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func messageGetEnabledModules() (out map[string]func(Message) *MessageCheckResult) {
	out = make(map[string]func(Message) *MessageCheckResult)

	if Config.Quotas.Enabled {
		out["quotas"] = quotasIsAllowed
	}

	if Config.SpamAssassin.Enabled {
		out["spamassassin"] = saGetResult
	}

	if Config.Greylisting.Enabled {
		out["greylisting"] = greylistGetResult
	}

	return
}

func messageSave(msg Message) {
	sess := *msg.getSession()

	var sender_local, sender_domain string
	if strings.Index(msg.getFrom(), "@") != -1 {
		sender_local = strings.Split(msg.getFrom(), "@")[0]
		sender_domain = strings.Split(msg.getFrom(), "@")[1]
	} else {
		sender_local = msg.getFrom()
	}

	messageIdHdr := ""
	for _, v := range msg.getHeaders() {
		if strings.EqualFold((*v).getKey(), "Message-Id") {
			messageIdHdr = (*v).getValue()
		}
	}

	if messageIdHdr == "" {
		messageIdHdr = fmt.Sprintf("<%d.%s.cluegetter@%s>",
			time.Now().Unix(), msg.getQueueId(), sess.getMtaHostName())
		msg.setInjectMessageId(messageIdHdr)
	}

	StatsCounters["RdbmsQueries"].increase(1)
	_, err := MessageInsertMsgStmt.Exec(
		msg.getQueueId(),
		sess.getId(),
		time.Now(),
		messageIdHdr,
		sender_local,
		sender_domain,
		msg.getRcptCount(),
	)

	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
	}

	if Config.ClueGetter.Archive_Retention_Body > 0 {
		messageSaveBody(msg)
	}

	messageSaveRecipients(msg.getRecipients(), msg.getQueueId())
	if Config.ClueGetter.Archive_Retention_Header > 0 {
		messageSaveHeaders(msg)
	}
}

func messageSaveBody(msg Message) {
	for key, value := range msg.getBody() {
		StatsCounters["RdbmsQueries"].increase(1)
		_, err := MessageInsertMsgBodyStmt.Exec(
			msg.getQueueId(),
			key,
			value,
		)

		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
		}
	}
}

func messageSaveRecipients(recipients []string, msgId string) {
	for _, rcpt := range recipients {
		var local string
		var domain string

		if strings.Index(rcpt, "@") != -1 {
			local = strings.SplitN(rcpt, "@", 2)[0]
			domain = strings.SplitN(rcpt, "@", 2)[1]
		} else {
			local = rcpt
			domain = ""
		}

		StatsCounters["RdbmsQueries"].increase(1)
		res, err := MessageInsertRcptStmt.Exec(local, domain)
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			panic("Could not execute MessageInsertRcptStmt in messageSaveRecipients(). Error: " + err.Error())
		}

		rcptId, err := res.LastInsertId()
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			panic("Could not get lastinsertid from MessageInsertRcptStmt in messageSaveRecipients(). Error: " + err.Error())
		}

		StatsCounters["RdbmsQueries"].increase(1)
		_, err = MessageInsertMsgRcptStmt.Exec(msgId, rcptId)
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			panic("Could not get execute MessageInsertMsgRcptStmt in messageSaveRecipients(). Error: " + err.Error())
		}
	}
}

func messageSaveHeaders(msg Message) {
	for _, headerPair := range msg.getHeaders() {
		StatsCounters["RdbmsQueries"].increase(1)
		_, err := MessageInsertMsgHdrStmt.Exec(
			msg.getQueueId(), (*headerPair).getKey(), (*headerPair).getValue())

		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
		}
	}
}

func messageGetHeadersToAdd(msg Message, results [3][]*MessageCheckResult) []*milterMessageHeader {
	sess := *msg.getSession()
	out := make([]*milterMessageHeader, len(MessageInsertHeaders))
	copy(out, MessageInsertHeaders)

	if Config.ClueGetter.Add_Header_X_Spam_Score {
		spamscore := 0.0
		for _, result := range results[messageReject] {
			spamscore += result.score
		}

		out = append(out, &milterMessageHeader{"X-Spam-Score", fmt.Sprintf("%.2f", spamscore)})
	}

	if Config.ClueGetter.Insert_Missing_Message_Id == true && msg.getInjectMessageId() != "" {
		out = append(out, &milterMessageHeader{"Message-Id", msg.getInjectMessageId()})
	}

	for k, v := range out {
		out[k].Value = strings.Replace(v.Value, "%h", sess.getMtaHostName(), -1)
	}

	return out
}

func messagePrune() {
	ticker := time.NewTicker(30 * time.Minute)

	var prunables = []struct {
		stmt      *sql.Stmt
		descr     string
		retention float64
	}{
		{MessagePruneBodyStmt, "bodies", Config.ClueGetter.Archive_Retention_Body},
		{MessagePruneHeaderStmt, "headers", Config.ClueGetter.Archive_Retention_Header},
		{MessagePruneMessageResultStmt, "message results", Config.ClueGetter.Archive_Retention_Message_Result},
		{MessagePruneMessageQuotaStmt, "message-quota relations", Config.ClueGetter.Archive_Retention_Message},
		{MessagePruneMessageRecipient, "message-recipient relations", Config.ClueGetter.Archive_Retention_Message},
		{MessagePruneMessageStmt, "messages", Config.ClueGetter.Archive_Retention_Message},
		{MessagePruneRecipient, "recipients", Config.ClueGetter.Archive_Retention_Message},
	}

WaitForNext:
	for {
		select {
		case <-ticker.C:
			t0 := time.Now()
			Log.Info("Pruning some old data now")

			for _, prunable := range prunables {
				if prunable.retention < Config.ClueGetter.Archive_Retention_Safeguard {
					Log.Info("Not pruning %s because its retention (%.2f weeks) is lower than the safeguard (%.2f)",
						prunable.descr, prunable.retention, Config.ClueGetter.Archive_Retention_Safeguard)
					continue
				}

				tStart := time.Now()
				res, err := prunable.stmt.Exec(t0, prunable.retention, instance)
				if err != nil {
					Log.Error("Could not prune %s: %s", prunable.descr, err.Error())
					continue WaitForNext
				}

				rowCnt, err := res.RowsAffected()
				if err != nil {
					Log.Error("Error while fetching number of affected rows: ", err)
					continue WaitForNext
				}

				Log.Info("Pruned %d %s in %s", rowCnt, prunable.descr, time.Now().Sub(tStart).String())
			}
		}
	}
}
