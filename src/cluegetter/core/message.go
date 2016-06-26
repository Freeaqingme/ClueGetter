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
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MessagePermit = iota
	MessageTempFail
	MessageReject
	MessageError
)

type Message struct {
	session *MilterSession

	QueueId  string
	From     *address.Address
	Rcpt     []*address.Address
	Headers  []MessageHeader
	Body     []byte `json:"-"`
	Date     time.Time
	BodySize int
	BodyHash string

	Verdict                int
	VerdictMsg             string
	RejectScore            float64
	RejectScoreThreshold   float64
	TempfailScore          float64
	TempfailScoreThreshold float64

	CheckResults []*MessageCheckResult

	injectMessageId string
}

func (msg *Message) Session() *MilterSession {
	return msg.session
}

func (msg *Message) SetSession(s *MilterSession) {
	if msg.session != nil {
		panic("Cannot set session because session is already set")
	}
	msg.session = s
}

type MessageHeader struct {
	Key        string
	Value      string
	milterIdx  int
	flagUnique bool
	deleted    bool
}

func (h *MessageHeader) getKey() string {
	return h.Key
}

func (h *MessageHeader) getValue() string {
	return h.Value
}

type MessageCheckResult struct {
	Module          string
	SuggestedAction int
	Message         string
	Score           float64
	Determinants    map[string]interface{}
	Duration        time.Duration
	WeightedScore   float64
	Callbacks       []*func(*Message, int)
}

func (cr *MessageCheckResult) MarshalJSON() ([]byte, error) {
	type Alias MilterSession

	determinants, _ := json.Marshal(cr.Determinants)
	out := &struct {
		Module        string
		Verdict       int
		Message       string
		Score         float64
		Determinants  string
		Duration      time.Duration
		WeightedScore float64
	}{
		Module:        cr.Module,
		Verdict:       cr.SuggestedAction,
		Message:       cr.Message,
		Score:         cr.Score,
		Determinants:  string(determinants),
		Duration:      cr.Duration,
		WeightedScore: cr.WeightedScore,
	}

	return json.Marshal(out)
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

var MessageInsertHeaders = make([]MessageHeader, 0)
var MessageModuleGroups = make([]*MessageModuleGroup, 0)

func messageStart() {
	for _, hdrString := range Config.ClueGetter.Add_Header {
		if strings.Index(hdrString, ":") < 1 {
			Log.Fatal("Invalid header specified: ", hdrString)
		}

		header := MessageHeader{
			Key:   strings.Trim(strings.SplitN(hdrString, ":", 2)[0], " "),
			Value: strings.Trim(strings.SplitN(hdrString, ":", 2)[1], " "),
		}

		flagsPosStart := strings.Index(header.Key, "[")
		flagsPosEnd := strings.Index(header.Key, "]")
		if flagsPosStart == 0 && flagsPosEnd != -1 {
			for _, flag := range strings.Split(header.Key[1:flagsPosEnd], ",") {
				switch flag {
				case "U":
					header.flagUnique = true
				default:
					Log.Fatal("Unrecognized flag: " + flag)
				}
			}
			header.Key = strings.Trim(header.Key[flagsPosEnd+1:len(header.Key)], " ")
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
		verdict = MessageTempFail
		msgStr = "An internal error occurred."
		return
	}()

	flatResults := make([]*MessageCheckResult, 0)
	results[MessagePermit] = make([]*MessageCheckResult, 0)
	results[MessageTempFail] = make([]*MessageCheckResult, 0)
	results[MessageReject] = make([]*MessageCheckResult, 0)
	results[MessageError] = make([]*MessageCheckResult, 0)

	var breakerScore [4]float64
	done := make(chan bool)
	errorCount := 0
	resultsChan := messageGetResults(msg, done)
	for result := range resultsChan {
		if result.Score == 0.0 {
			result.SuggestedAction = MessagePermit // This is purely aesthetic but prevents confusion
		}

		results[result.SuggestedAction] = append(results[result.SuggestedAction], result)
		flatResults = append(flatResults, result)
		breakerScore[result.SuggestedAction] += result.Score
		result.WeightedScore = result.Score

		if result.SuggestedAction == MessageError {
			errorCount = errorCount + 1
		} else if breakerScore[result.SuggestedAction] >= msg.session.config.ClueGetter.Breaker_Score {
			Log.Debug(
				"Breaker score %.2f/%.2f reached. Aborting all running modules",
				breakerScore[result.SuggestedAction],
				msg.session.config.ClueGetter.Breaker_Score,
			)

			go func() {
				for _ = range resultsChan {
					// Allow other modules to finish and flush through the channel
					// It will be closed in messageGetResults() once all are finished.
				}
			}()
			break
		}
	}
	close(done)

	errorCount = errorCount - messageWeighResults(flatResults)

	messageEnsureHasMessageId(msg)

	getDecidingResultWithMessage := func(results []*MessageCheckResult) *MessageCheckResult {
		out := results[0]
		maxScore := float64(0)
		for _, result := range results {
			if result.WeightedScore > maxScore && result.Message != "" {
				out = result
				maxScore = result.WeightedScore
			}
		}
		return out
	}

	var totalScores [4]float64
	for _, result := range flatResults {
		totalScores[result.SuggestedAction] += result.WeightedScore
	}

	verdict = MessagePermit
	statusMsg := ""

	sconf := msg.session.config
	if totalScores[MessageReject] >= sconf.ClueGetter.Message_Reject_Score {
		StatsCounters["MessageVerdictReject"].increase(1)
		verdict = MessageReject
		statusMsg = getDecidingResultWithMessage(results[MessageReject]).Message
	} else if errorCount > 0 {
		statusMsg = "An internal server error ocurred"
		verdict = MessageTempFail
	} else if (totalScores[MessageTempFail] + totalScores[MessageReject]) >= sconf.ClueGetter.Message_Tempfail_Score {
		StatsCounters["MessageVerdictTempfail"].increase(1)
		verdict = MessageTempFail
		statusMsg = getDecidingResultWithMessage(results[MessageTempFail]).Message
	}

	if verdict != MessagePermit && statusMsg == "" {
		statusMsg = "Reason Unspecified"
	}

	for _, result := range flatResults {
		for _, callback := range result.Callbacks {
			go func(callback *func(*Message, int), msg *Message, verdict int) {
				defer func() {
					if Config.ClueGetter.Exit_On_Panic {
						return
					}
					r := recover()
					if r == nil {
						return
					}
					Log.Error("Panic caught in callback in messageGetVerdict(). Recovering. Error: %s", r)
				}()
				(*callback)(msg, verdict)
			}(callback, msg, verdict)
		}
	}

	msg.Verdict = verdict
	msg.VerdictMsg = statusMsg
	msg.RejectScore = totalScores[MessageReject]
	msg.RejectScoreThreshold = sconf.ClueGetter.Message_Reject_Score
	msg.TempfailScore = totalScores[MessageTempFail]
	msg.TempfailScoreThreshold = sconf.ClueGetter.Message_Tempfail_Score
	msg.CheckResults = flatResults

	messageSave(msg)
	return verdict, statusMsg, results
}

func messageWeighResults(results []*MessageCheckResult) (ignoreErrorCount int) {
	ignoreErrorCount = 0
	for _, moduleGroup := range MessageModuleGroups {
		totalWeight := 0.0
		moduleGroupErrorCount := 0

		for _, moduleResult := range results {
			for _, moduleGroupModule := range moduleGroup.modules {
				if moduleResult.Module != moduleGroupModule.module {
					continue
				}

				if moduleResult.SuggestedAction == MessageError {
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
				if moduleResult.Module != moduleGroupModule.module ||
					moduleResult.SuggestedAction == MessageError {
					continue
				}

				moduleResult.WeightedScore = moduleResult.WeightedScore * moduleGroupModule.weight * multiply
			}
		}
	}
	return
}

func messageGetResults(msg *Message, done chan bool) chan *MessageCheckResult {
	var wg sync.WaitGroup
	out := make(chan *MessageCheckResult)

	for _, module := range cg.Modules() {
		wg.Add(1)
		callback := module.MessageCheck
		go func(moduleName string, moduleCallback *func(*Message, chan bool) *MessageCheckResult) {
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
					Module:          moduleName,
					SuggestedAction: MessageError,
					Message:         "An internal error ocurred",
					Score:           25,
					Determinants:    determinants,
					Duration:        time.Now().Sub(t0),
				}
			}()

			res := (*moduleCallback)(msg, done)
			if res != nil {
				res.Duration = time.Now().Sub(t0)
				out <- res
			}
		}(module.Name(), &callback)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func messageSave(msg *Message) {
	checkResults := make([]*Proto_Message_CheckResult, 0)
	for _, result := range msg.CheckResults {
		determinants, _ := json.Marshal(result.Determinants)

		duration := result.Duration.Seconds()
		verdict := Proto_Message_Verdict(result.SuggestedAction)
		protoStruct := &Proto_Message_CheckResult{
			MessageId:     msg.QueueId,
			Module:        result.Module,
			Verdict:       verdict,
			Score:         result.Score,
			WeightedScore: result.WeightedScore,
			Duration:      duration,
			Determinants:  determinants,
		}

		checkResults = append(checkResults, protoStruct)
	}

	headers := make([]*Proto_Message_Header, len(msg.Headers))
	for k, v := range msg.Headers {
		headerKey := v.getKey()
		headerValue := v.getValue()
		headers[k] = &Proto_Message_Header{Key: headerKey, Value: headerValue}
	}

	rcpt := []string{}
	for _, v := range msg.Rcpt {
		rcpt = append(rcpt, v.String())
	}

	verdictEnum := Proto_Message_Verdict(msg.Verdict)
	protoStruct := &Proto_Message{
		Id:                     msg.QueueId,
		From:                   msg.From.String(),
		Rcpt:                   rcpt,
		Headers:                headers,
		Body:                   msg.Body,
		Verdict:                verdictEnum,
		VerdictMsg:             msg.VerdictMsg,
		RejectScore:            msg.RejectScore,
		RejectScoreThreshold:   msg.RejectScoreThreshold,
		TempfailScore:          msg.TempfailScore,
		TempfailScoreThreshold: msg.TempfailScoreThreshold,
		CheckResults:           checkResults,
		Session:                msg.session.getProtoBufStruct(),
	}

	protoMsg, err := protoStruct.Marshal()
	if err != nil {
		panic("marshaling error: " + err.Error())
	}

	messagePersistQueue <- protoMsg
	go messagePersistInCache(msg.QueueId, messageGetMessageId(msg), protoMsg)
}

func messageGetMutableHeaders(msg *Message, results [4][]*MessageCheckResult) (add, delete []MessageHeader) {
	sess := *msg.session
	add = make([]MessageHeader, len(MessageInsertHeaders))
	copy(add, MessageInsertHeaders)

	rejectscore := 0.0
	for _, result := range results[MessageReject] {
		rejectscore += result.WeightedScore
	}

	// Add the recipients, duplicate lines if there's >1 recipients
	for k, v := range add {
		if strings.Index(v.Value, "%{recipient}") == -1 {
			continue
		}

		for _, rcpt := range msg.Rcpt[1:] {
			value := strings.Replace(add[k].Value, "%{recipient}", rcpt.String(), -1)
			add = append(add, MessageHeader{Key: v.Key, Value: value})
		}

		add[k].Value = strings.Replace(add[k].Value, "%{recipient}", msg.Rcpt[0].String(), -1)
	}

	if msg.session.config.ClueGetter.Insert_Missing_Message_Id == true && msg.injectMessageId != "" {
		add = append(add, MessageHeader{Key: "Message-Id", Value: msg.injectMessageId})
	}

	for k, v := range add {
		if v.flagUnique {
			delete = append(delete, msg.GetHeader(v.getKey(), false)...)
		}

		add[k].Value = strings.Replace(add[k].Value, "%{hostname}", sess.getMtaHostName(), -1)
		add[k].Value = strings.Replace(add[k].Value, "%{rejectScore}", fmt.Sprintf("%.2f", rejectscore), -1)

		if rejectscore >= msg.session.config.ClueGetter.Message_Spamflag_Score {
			add[k].Value = strings.Replace(add[k].Value, "%{spamFlag}", "YES", -1)
		} else {
			add[k].Value = strings.Replace(add[k].Value, "%{spamFlag}", "NO", -1)
		}
	}

	deleted := 0
	for k := range add {
		k -= deleted
		if add[k].Value != "" {
			continue
		}

		deleted += 1
		if len(add) > k {
			add = append(add[:k], add[k+1:]...)
		} else {
			add = add[:k]
		}
	}

	return add, delete
}

func (msg *Message) GetHeader(key string, includeDeleted bool) []MessageHeader {
	out := make([]MessageHeader, 0)
	for _, hdr := range msg.Headers {
		if strings.EqualFold(hdr.Key, key) && (includeDeleted || !hdr.deleted) {
			out = append(out, hdr)
		}
	}

	return out
}

func (msg *Message) String() []byte {
	sess := *msg.session
	fqdn := hostname
	revdns, err := net.LookupAddr(sess.getIp())
	revdnsStr := "unknown"
	if err == nil {
		revdnsStr = strings.Join(revdns, "")
	}

	body := make([]string, 0)

	body = append(body, fmt.Sprintf("Received: from %s (%s [%s])\r\n\tby %s with SMTP id %s; %s\r\n",
		sess.getHelo(),
		revdnsStr,
		sess.getIp(),
		fqdn,
		msg.QueueId,
		time.Now().Format(time.RFC1123Z)))

	for _, header := range msg.Headers {
		body = append(body, (header).getKey()+": "+(header).getValue()+"\r\n")
	}

	body = append(body, "\r\n")
	body = append(body, string(msg.Body))

	return []byte(strings.Join(body, ""))
}

func messageEnsureHasMessageId(msg *Message) {
	for _, v := range msg.Headers {
		if strings.EqualFold((v).getKey(), "Message-Id") {
			return
		}
	}

	id := messageGetMessageId(msg)
	msg.Headers = append(msg.Headers, MessageHeader{
		Key: "Message-Id", Value: id,
	})
}

func messageGetMessageId(msg *Message) string {
	sess := msg.session

	messageIdHdr := ""
	for _, v := range msg.Headers {
		if strings.EqualFold((v).getKey(), "Message-Id") {
			return (v).getValue()
		}
	}

	if msg.injectMessageId == "" {
		messageIdHdr = messageGenerateMessageId(msg.QueueId, sess.getMtaHostName())
		msg.injectMessageId = messageIdHdr
	}

	return msg.injectMessageId
}

func messageAcceptRecipient(rcpt *address.Address) (finalVerdict int, finalMsg string) {
	finalVerdict = MessagePermit
	finalMsg = ""

	for _, module := range cg.Modules() {
		verdict, msg := module.RecipientCheck(rcpt)
		if verdict == MessageError {
			return verdict, msg
		}

		if verdict > finalVerdict {
			finalVerdict = verdict
			finalMsg = msg
		}
	}

	return
}

func messageGenerateMessageId(queueId, host string) string {
	if host != "" {
		host = hostname
	}

	return fmt.Sprintf("<%d.%s.cluegetter@%s>",
		time.Now().Unix(), queueId, hostname)
}

// Deprecated: Use package address instead
func messageParseAddress(address string, singleIsUser bool) (local, domain string) {
	if strings.Index(address, "@") != -1 {
		local = strings.SplitN(address, "@", 2)[0]
		domain = strings.SplitN(address, "@", 2)[1]
	} else if singleIsUser {
		local = address
		domain = ""
	} else {
		local = ""
		domain = address
	}

	return
}
