// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	messagePermit = iota
	messageTempFail
	messageReject
	messageError
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
	getReverseDns() string
	getHostname() string
	getHelo() string
	getMtaHostName() string
	getMtaDaemonName() string
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

	String(bool) []byte
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
	weightedScore   float64
}

type MessageModuleGroup struct {
	modules     []*MessageModuleGroupMember
	name        string
	totalWeight float64
}

type MessageModuleGroupMember struct {
	module string
	weight float64
}

var MessageInsertHeaders = make([]*milterMessageHeader, 0)
var MessageModuleGroups = make([]*MessageModuleGroup, 0)

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

	messageStartModuleGroups()
	messageStmtStart()
	go messagePrune()

	Log.Info("Message handler started successfully")
}

func messageStop() {
	MessageStmtInsertMsg.Close()
	Log.Info("Message handler stopped successfully")
}

func messageStartModuleGroups() {
	modules := map[string]bool{
		"quotas": true, "spamassassin": true, "greylisting": true, "rspamd": true,
	}
	for groupName, groupConfig := range Config.ModuleGroup {
		group := &MessageModuleGroup{
			modules:     make([]*MessageModuleGroupMember, len((*groupConfig).Module)),
			name:        groupName,
			totalWeight: 0,
		}
		MessageModuleGroups = append(MessageModuleGroups, group)
		if len((*groupConfig).Module) == 0 {
			Log.Fatal(fmt.Sprintf("Config Error: Module Group %s does not have any modules", groupName))
		}

		for k, v := range (*groupConfig).Module {
			split := strings.SplitN(v, " ", 2)
			if len(split) < 2 {
				Log.Fatal(fmt.Sprintf("Config Error: Incorrectly formatted module group %s/%s", groupName, v))
			}
			if !modules[split[1]] {
				Log.Fatal(fmt.Sprintf("Unknown module specified for module group %s: %s", groupName, split[1]))
			}

			weight, err := strconv.ParseFloat(split[0], 64)
			if err != nil {
				Log.Fatal(fmt.Sprintf("Invalid weight specified in module group %s/%s", groupName, split[1]))
			}

			for _, existingGroup := range MessageModuleGroups {
				for _, existingModuleGroupModule := range existingGroup.modules {
					if existingModuleGroupModule != nil && split[1] == existingModuleGroupModule.module {
						Log.Fatal(fmt.Sprintf("Module %s is already part of module group '%s', cannot add to '%s'",
							split[1], existingGroup.name, groupName,
						))
					}
				}
			}

			group.totalWeight = group.totalWeight + weight
			group.modules[k] = &MessageModuleGroupMember{
				module: split[1],
				weight: weight,
			}
		}
	}
}

func messageGetVerdict(msg Message) (verdict int, msgStr string, results [4][]*MessageCheckResult) {
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

	flatResults := make([]*MessageCheckResult, 0)
	results[messagePermit] = make([]*MessageCheckResult, 0)
	results[messageTempFail] = make([]*MessageCheckResult, 0)
	results[messageReject] = make([]*MessageCheckResult, 0)
	results[messageError] = make([]*MessageCheckResult, 0)

	var breakerScore [4]float64
	done := make(chan bool)
	errorCount := 0
	for result := range messageGetResults(msg, done) {
		results[result.suggestedAction] = append(results[result.suggestedAction], result)
		flatResults = append(flatResults, result)
		breakerScore[result.suggestedAction] += result.score
		result.weightedScore = result.score

		if result.suggestedAction == messageError {
			errorCount = errorCount + 1
		} else if breakerScore[result.suggestedAction] >= Config.ClueGetter.Breaker_Score {
			Log.Debug(
				"Breaker score %.2f/%.2f reached. Aborting all running modules",
				breakerScore[result.suggestedAction],
				Config.ClueGetter.Breaker_Score,
			)
			break
		}
	}
	close(done)

	errorCount = errorCount - messageWeighResults(flatResults)

	verdictValue := [4]string{"permit", "tempfail", "reject", "error"}
	if Config.ClueGetter.Archive_Retention_Message_Result > 0 {
		for _, result := range flatResults {
			determinants, _ := json.Marshal(result.determinants)

			StatsCounters["RdbmsQueries"].increase(1)
			_, err := MessageStmtInsertModuleResult.Exec(
				msg.getQueueId(), result.module, verdictValue[result.suggestedAction],
				result.score, result.weightedScore, result.duration.Seconds(), determinants)
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
			if result.weightedScore > maxScore && result.message != "" {
				out = result
				maxScore = result.weightedScore
			}
		}
		return out
	}

	var totalScores [4]float64
	for _, result := range flatResults {
		totalScores[result.suggestedAction] += result.weightedScore
	}

	if totalScores[messageReject] >= Config.ClueGetter.Message_Reject_Score {
		determinant := getDecidingResultWithMessage(results[messageReject])
		StatsCounters["MessageVerdictReject"].increase(1)
		messageSaveVerdict(msg, messageReject, determinant.message, totalScores[messageReject], totalScores[messageTempFail])
		return messageReject, determinant.message, results
	}
	if errorCount > 0 {
		errorMsg := "An internal server error ocurred"
		messageSaveVerdict(msg, messageTempFail, errorMsg, totalScores[messageReject], totalScores[messageTempFail])
		return messageTempFail, errorMsg, results
	}
	if (totalScores[messageTempFail] + totalScores[messageReject]) >= Config.ClueGetter.Message_Tempfail_Score {
		determinant := getDecidingResultWithMessage(results[messageTempFail])
		StatsCounters["MessageVerdictTempfail"].increase(1)
		messageSaveVerdict(msg, messageTempFail, determinant.message, totalScores[messageReject], totalScores[messageTempFail])
		return messageTempFail, determinant.message, results
	}

	StatsCounters["MessageVerdictPermit"].increase(1)
	messageSaveVerdict(msg, messagePermit, "", totalScores[messageReject], totalScores[messageTempFail])
	verdict = messagePermit
	msgStr = ""
	return
}

func messageWeighResults(results []*MessageCheckResult) (ignoreErrorCount int) {
	ignoreErrorCount = 0
	for _, moduleGroup := range MessageModuleGroups {
		totalWeight := 0.0
		moduleGroupErrorCount := 0

		for _, moduleResult := range results {
			for _, moduleGroupModule := range moduleGroup.modules {
				if moduleResult.module != moduleGroupModule.module {
					continue
				}

				if moduleResult.suggestedAction == messageError {
					moduleGroupErrorCount = moduleGroupErrorCount + 1
				} else {
					totalWeight = totalWeight + moduleGroupModule.weight
				}
			}
		}

		if moduleGroupErrorCount != len(moduleGroup.modules) {
			ignoreErrorCount = ignoreErrorCount + moduleGroupErrorCount
		} else {
			continue
		}

		multiply := 1.0 * (moduleGroup.totalWeight / totalWeight)
		for _, moduleResult := range results {
			for _, moduleGroupModule := range moduleGroup.modules {
				if moduleResult.module != moduleGroupModule.module ||
					moduleResult.suggestedAction == messageError {
					continue
				}

				moduleResult.weightedScore = moduleResult.weightedScore * moduleGroupModule.weight * multiply
			}
		}
	}
	return
}

func messageSaveVerdict(msg Message, verdict int, verdictMsg string, rejectScore float64, tempfailScore float64) {
	verdictValue := [3]string{"permit", "tempfail", "reject"}

	StatsCounters["RdbmsQueries"].increase(1)
	_, err := MessageStmtSetVerdict.Exec(
		verdictValue[verdict],
		verdictMsg,
		rejectScore,
		Config.ClueGetter.Message_Reject_Score,
		tempfailScore,
		Config.ClueGetter.Message_Tempfail_Score,
		msg.getQueueId(),
	)

	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
	}
}

func messageGetBodyTotals(msg Message) (length uint32, md5Sum string) {
	length = 0
	h := md5.New()
	for _, chunk := range msg.getBody() {
		length = length + uint32(len(chunk))
		io.WriteString(h, chunk+"\r\n")
	}

	return length, fmt.Sprintf("%x", h.Sum(nil))
}

func messageGetResults(msg Message, done chan bool) chan *MessageCheckResult {
	var wg sync.WaitGroup
	out := make(chan *MessageCheckResult)

	modules := messageGetEnabledModules()
	for moduleName, moduleCallback := range modules {
		wg.Add(1)
		go func(moduleName string, moduleCallback func(Message, chan bool) *MessageCheckResult) {
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
					suggestedAction: messageError,
					message:         "An internal error ocurred",
					score:           25,
					determinants:    determinants,
					duration:        time.Now().Sub(t0),
				}
			}()

			res := moduleCallback(msg, done)
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

func messageGetEnabledModules() (out map[string]func(Message, chan bool) *MessageCheckResult) {
	out = make(map[string]func(Message, chan bool) *MessageCheckResult)

	if Config.Quotas.Enabled {
		out["quotas"] = quotasIsAllowed
	}

	if Config.SpamAssassin.Enabled {
		out["spamassassin"] = saGetResult
	}

	if Config.Rspamd.Enabled {
		out["rspamd"] = rspamdGetResult
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

	size, hash := messageGetBodyTotals(msg)
	StatsCounters["RdbmsQueries"].increase(1)
	_, err := MessageStmtInsertMsg.Exec(
		msg.getQueueId(),
		sess.getId(),
		time.Now(),
		size,
		hash,
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
		_, err := MessageStmtInsertMsgBody.Exec(
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
		res, err := MessageStmtInsertRcpt.Exec(local, domain)
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			panic("Could not execute MessageStmtInsertRcpt in messageSaveRecipients(). Error: " + err.Error())
		}

		rcptId, err := res.LastInsertId()
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			panic("Could not get lastinsertid from MessageStmtInsertRcpt in messageSaveRecipients(). Error: " + err.Error())
		}

		StatsCounters["RdbmsQueries"].increase(1)
		_, err = MessageStmtInsertMsgRcpt.Exec(msgId, rcptId)
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			panic("Could not get execute MessageStmtInsertMsgRcpt in messageSaveRecipients(). Error: " + err.Error())
		}
	}
}

func messageSaveHeaders(msg Message) {
	for _, headerPair := range msg.getHeaders() {
		StatsCounters["RdbmsQueries"].increase(1)
		_, err := MessageStmtInsertMsgHdr.Exec(
			msg.getQueueId(), (*headerPair).getKey(), (*headerPair).getValue())

		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
		}
	}
}

func messageGetHeadersToAdd(msg Message, results [4][]*MessageCheckResult) []*milterMessageHeader {
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
		{MessageStmtPruneBody, "bodies", Config.ClueGetter.Archive_Retention_Body},
		{MessageStmtPruneHeader, "headers", Config.ClueGetter.Archive_Retention_Header},
		{MessageStmtPruneMessageResult, "message results", Config.ClueGetter.Archive_Retention_Message_Result},
		{MessageStmtPruneMessageQuota, "message-quota relations", Config.ClueGetter.Archive_Retention_Message},
		{MessageStmtPruneMessageRecipient, "message-recipient relations", Config.ClueGetter.Archive_Retention_Message},
		{MessageStmtPruneMessage, "messages", Config.ClueGetter.Archive_Retention_Message},
		{MessageStmtPruneSession, "sessions", Config.ClueGetter.Archive_Retention_Message},
		{MessageStmtPruneRecipient, "recipients", Config.ClueGetter.Archive_Retention_Message},
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

func (msg milterMessage) String(includeRcvdByHdr bool) []byte {
	sess := *msg.getSession()
	fqdn, err := os.Hostname()
	if err != nil {
		Log.Error("Could not determine FQDN")
		fqdn = sess.getMtaHostName()
	}
	revdns, err := net.LookupAddr(sess.getIp())
	revdnsStr := "unknown"
	if err == nil {
		revdnsStr = strings.Join(revdns, "")
	}

	body := make([]string, 0)

	if includeRcvdByHdr {
		body = append(body, fmt.Sprintf("Received: from %s (%s [%s])\r\n\tby %s with SMTP id %d@%s; %s",
			sess.getHelo(),
			revdnsStr,
			sess.getIp(),
			fqdn,
			sess.getId(),
			fqdn,
			time.Now().Format(time.RFC1123Z)))
	}

	for _, header := range msg.getHeaders() {
		body = append(body, (*header).getKey()+": "+(*header).getValue())
	}

	body = append(body, "")
	for _, bodyChunk := range msg.getBody() {
		body = append(body, bodyChunk)
	}

	return []byte(strings.Join(body, "\r\n"))
}
