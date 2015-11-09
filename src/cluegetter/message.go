// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"encoding/json"
	"fmt"
	"github.com/golang/protobuf/proto"
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

type Message struct {
	session *milterSession

	QueueId string
	From    string
	Rcpt    []string
	Headers []*MessageHeader
	Body    []byte

	injectMessageId string
}

type MessageHeader struct {
	Key   string
	Value string
}

func (h *MessageHeader) getKey() string {
	return h.Key
}

func (h *MessageHeader) getValue() string {
	return h.Value
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

var MessageInsertHeaders = make([]*MessageHeader, 0)
var MessageModuleGroups = make([]*MessageModuleGroup, 0)

func messageStart() {
	for _, hdrString := range Config.ClueGetter.Add_Header {
		if strings.Index(hdrString, ":") < 1 {
			Log.Fatal("Invalid header specified: ", hdrString)
		}

		header := &MessageHeader{
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
	messagePersistStart()

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

func messageGetVerdict(msg *Message) (verdict int, msgStr string, results [4][]*MessageCheckResult) {
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

	checkResults := make([]*Proto_MessageV1_CheckResult, 0)
	if Config.ClueGetter.Archive_Retention_Message_Result > 0 {
		for _, result := range flatResults {
			determinants, _ := json.Marshal(result.determinants)

			duration := result.duration.Seconds()
			verdict := Proto_MessageV1_Verdict(result.suggestedAction)
			protoStruct := &Proto_MessageV1_CheckResult{
				MessageId:     &msg.QueueId,
				Module:        &result.module,
				Verdict:       &verdict,
				Score:         &result.score,
				WeightedScore: &result.weightedScore,
				Duration:      &duration,
				Determinants:  determinants,
			}

			checkResults = append(checkResults, protoStruct)
		}
	}

	messageEnsureHasMessageId(msg)

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
		messageSave(msg, checkResults, messageReject, determinant.message, totalScores[messageReject], totalScores[messageTempFail])
		return messageReject, determinant.message, results
	}
	if errorCount > 0 {
		errorMsg := "An internal server error ocurred"
		messageSave(msg, checkResults, messageTempFail, errorMsg, totalScores[messageReject], totalScores[messageTempFail])
		return messageTempFail, errorMsg, results
	}
	if (totalScores[messageTempFail] + totalScores[messageReject]) >= Config.ClueGetter.Message_Tempfail_Score {
		determinant := getDecidingResultWithMessage(results[messageTempFail])
		StatsCounters["MessageVerdictTempfail"].increase(1)
		messageSave(msg, checkResults, messageTempFail, determinant.message, totalScores[messageReject], totalScores[messageTempFail])
		return messageTempFail, determinant.message, results
	}

	StatsCounters["MessageVerdictPermit"].increase(1)
	messageSave(msg, checkResults, messagePermit, "", totalScores[messageReject], totalScores[messageTempFail])
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

func messageGetResults(msg *Message, done chan bool) chan *MessageCheckResult {
	var wg sync.WaitGroup
	out := make(chan *MessageCheckResult)

	modules := messageGetEnabledModules()
	for moduleName, moduleCallback := range modules {
		wg.Add(1)
		go func(moduleName string, moduleCallback func(*Message, chan bool) *MessageCheckResult) {
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

func messageGetEnabledModules() (out map[string]func(*Message, chan bool) *MessageCheckResult) {
	out = make(map[string]func(*Message, chan bool) *MessageCheckResult)

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

func messageSave(msg *Message, checkResults []*Proto_MessageV1_CheckResult, verdict int,
	verdictMsg string, rejectScore float64, tempfailScore float64) {

	headers := make([]*Proto_MessageV1_Header, len(msg.Headers))
	for k, v := range msg.Headers {
		headerKey := v.getKey()
		headerValue := v.getValue()
		headers[k] = &Proto_MessageV1_Header{Key: &headerKey, Value: &headerValue}
	}

	sess := msg.session
	timeStart := uint64(sess.timeStart.Unix())
	var timeEnd uint64
	if &sess.timeEnd != nil {
		timeEnd = uint64(sess.timeEnd.Unix())
	}

	verdictEnum := Proto_MessageV1_Verdict(verdict)
	protoStruct := &Proto_MessageV1{
		Id:                     &msg.QueueId,
		From:                   &msg.From,
		Rcpt:                   msg.Rcpt,
		Headers:                headers,
		Body:                   msg.Body,
		Verdict:                &verdictEnum,
		VerdictMsg:             &verdictMsg,
		RejectScore:            &rejectScore,
		RejectScoreThreshold:   &Config.ClueGetter.Message_Reject_Score,
		TempfailScore:          &tempfailScore,
		TempfailScoreThreshold: &Config.ClueGetter.Message_Tempfail_Score,
		CheckResults:           checkResults,
		Session: &Proto_MessageV1_Session{
			Id:            sess.id[:],
			TimeStart:     &timeStart,
			TimeEnd:       &timeEnd,
			SaslUsername:  &sess.SaslUsername,
			SaslSender:    &sess.SaslSender,
			SaslMethod:    &sess.SaslMethod,
			CertIssuer:    &sess.CertIssuer,
			CertSubject:   &sess.CertSubject,
			CipherBits:    &sess.CipherBits,
			Cipher:        &sess.Cipher,
			TlsVersion:    &sess.TlsVersion,
			Ip:            &sess.Ip,
			ReverseDns:    &sess.ReverseDns,
			Hostname:      &sess.Hostname,
			Helo:          &sess.Helo,
			MtaHostName:   &sess.Hostname,
			MtaDaemonName: &sess.MtaDaemonName,
		},
	}

	protoMsg, err := proto.Marshal(protoStruct)
	if err != nil {
		panic("marshaling error: " + err.Error())
	}

	messagePersistQueue <- protoMsg
}

func messageGetHeadersToAdd(msg *Message, results [4][]*MessageCheckResult) []*MessageHeader {
	sess := *msg.session
	out := make([]*MessageHeader, len(MessageInsertHeaders))
	copy(out, MessageInsertHeaders)

	if Config.ClueGetter.Add_Header_X_Spam_Score {
		spamscore := 0.0
		for _, result := range results[messageReject] {
			spamscore += result.score
		}

		out = append(out, &MessageHeader{"X-Spam-Score", fmt.Sprintf("%.2f", spamscore)})
	}

	if Config.ClueGetter.Insert_Missing_Message_Id == true && msg.injectMessageId != "" {
		out = append(out, &MessageHeader{"Message-Id", msg.injectMessageId})
	}

	for k, v := range out {
		out[k].Value = strings.Replace(v.Value, "%h", sess.getMtaHostName(), -1)
	}

	return out
}

func (msg Message) String() []byte {
	sess := *msg.session
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

	body = append(body, fmt.Sprintf("Received: from %s (%s [%s])\r\n\tby %s with SMTP id %d@%s; %s\r\n",
		sess.getHelo(),
		revdnsStr,
		sess.getIp(),
		fqdn,
		sess.getId(),
		fqdn,
		time.Now().Format(time.RFC1123Z)))

	for _, header := range msg.Headers {
		body = append(body, (*header).getKey()+": "+(*header).getValue()+"\r\n")
	}

	body = append(body, "\r\n")
	body = append(body, string(msg.Body))

	return []byte(strings.Join(body, ""))
}

func messageEnsureHasMessageId(msg *Message) {
	sess := msg.session

	messageIdHdr := ""
	for _, v := range msg.Headers {
		if strings.EqualFold((*v).getKey(), "Message-Id") {
			messageIdHdr = (*v).getValue()
			break
		}
	}

	if messageIdHdr == "" {
		messageIdHdr = fmt.Sprintf("<%d.%s.cluegetter@%s>",
			time.Now().Unix(), msg.QueueId, sess.getMtaHostName())
		msg.injectMessageId = messageIdHdr
		msg.Headers = append(msg.Headers, &MessageHeader{
			"Message-Id", messageIdHdr,
		})
	}
}
