// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"fmt"
	spamc "github.com/Freeaqingme/go-spamc"
	"strconv"
	"strings"
)

type saReport struct {
	score float64
	facts []*saReportFact
}

type saReportFact struct {
	Score       float64
	Symbol      string
	Description string
}

func init() {
	enable := func() bool { return Config.SpamAssassin.Enabled }
	milterCheck := saGetResult

	ModuleRegister(&ModuleOld{
		name:        "spamassassin",
		enable:      &enable,
		milterCheck: &milterCheck,
	})
}

func saGetResult(msg *Message, abort chan bool) *MessageCheckResult {
	if !msg.session.config.SpamAssassin.Enabled {
		return nil
	}

	rawReply, err := saGetRawReply(msg, abort)
	if err != nil || rawReply.Code != spamc.EX_OK {
		Log.Errorf("SpamAssassin returned an error: %s", err)
		return &MessageCheckResult{
			Module:          "spamassassin",
			SuggestedAction: MessageError,
			Message:         "An internal error occurred",
			Score:           25,
			Determinants:    map[string]interface{}{"error": err.Error()},
		}
	}

	Log.Debugf("Getting SA report for %s", msg.QueueId)
	report := saParseReply(rawReply)
	factsStr := func() []string {
		out := make([]string, 0)
		for _, fact := range report.facts {
			out = append(out, fmt.Sprintf("%s=%.3f", fact.Symbol, fact.Score))
		}
		return out
	}()

	Log.Debugf("Got SA score of %.2f for %s. Tests: [%s]", report.score, msg.QueueId, strings.Join(factsStr, ","))
	return &MessageCheckResult{
		Module:          "spamassassin",
		SuggestedAction: MessageReject,
		Message: "Our system has detected that this message is likely unsolicited mail (SPAM). " +
			"To reduce the amount of spam, this message has been blocked.",
		Score:        report.score,
		Determinants: map[string]interface{}{"report": report.facts},
	}
}

func saGetRawReply(msg *Message, abort chan bool) (*spamc.SpamDOut, error) {
	bodyStr := string(msg.String())

	host := Config.SpamAssassin.Host + ":" + strconv.Itoa(Config.SpamAssassin.Port)
	sconf := msg.session.config.SpamAssassin
	client := spamc.New(host, sconf.Timeout, sconf.Connect_Timeout)

	if len(bodyStr) > sconf.Max_Size {
		bodyStr = bodyStr[:sconf.Max_Size]
	}

	return client.Report(abort, bodyStr, msg.Rcpt[0].String())
}

/*
 The spamc client library returns a pretty shitty
 format, So we try to make the best of it and
 parse it into some nice structs.
*/
func saParseReply(reply *spamc.SpamDOut) *saReport {
	report := &saReport{facts: make([]*saReportFact, 0)}

	for key, value := range reply.Vars {
		if key == "spamScore" {
			report.score = value.(float64)
		} else if key == "report" {
			var reportFacts []map[string]interface{}
			reportFacts = value.([]map[string]interface{})
			for _, reportFact := range reportFacts {
				report.facts = append(report.facts, saParseReplyReportVar(reportFact))
			}
		}
	}

	return report
}

func saParseReplyReportVar(reportFactRaw map[string]interface{}) *saReportFact {
	reportFact := &saReportFact{}
	for key, value := range reportFactRaw {
		switch {
		case key == "score":
			reportFact.Score = value.(float64)
		case key == "symbol":
			reportFact.Symbol = value.(string)
		case key == "message":
			reportFact.Description = value.(string)
		}
	}

	return reportFact
}

func saLearn(msg *Proto_Message, spam bool) {
	if !Config.SpamAssassin.Enabled {
		return
	}

	bodyStr := string(bayesRenderProtoMsg(msg))

	host := Config.SpamAssassin.Host + ":" + strconv.Itoa(Config.SpamAssassin.Port)
	sconf := Config.SpamAssassin
	client := spamc.New(host, sconf.Timeout, sconf.Connect_Timeout)

	abort := make(chan bool) // unused really. To implement or not to implement. That's the question
	if spam {
		client.Learn(abort, spamc.LEARN_SPAM, bodyStr)
	} else {
		client.Learn(abort, spamc.LEARN_HAM, bodyStr)
	}
	close(abort)
}
