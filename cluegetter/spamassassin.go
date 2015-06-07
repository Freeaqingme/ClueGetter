// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

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

func saGetResult(msg Message) *MessageCheckResult {
	rawReply, err := saGetRawReply(msg)
	if err != nil || rawReply.Code != spamc.EX_OK {
		Log.Error("SpamAssassin returned an error: %s", err)
		return &MessageCheckResult{
			module:          "spamassassin",
			suggestedAction: messageTempFail,
			message:         "An internal error occurred",
			score:           10,
		}
	}

	Log.Debug("Getting SA report for %s", msg.getQueueId())
	report := saParseReply(rawReply)
	factsStr := func() []string {
		out := make([]string, 0)
		for _, fact := range report.facts {
			out = append(out, fmt.Sprintf("%s=%f", fact.Symbol, fact.Score))
		}
		return out
	}()

	Log.Debug("Got SA score of %f for %s. Tests: [%s]", report.score, msg.getQueueId(), strings.Join(factsStr, ","))
	return &MessageCheckResult{
		module:          "spamassassin",
		suggestedAction: messageReject,
		message: "Our system has detected that this message is likely unsolicited mail (SPAM). " +
			"To reduce the amount of spam, this message has been blocked.",
		score:        report.score,
		determinants: map[string]interface{}{"report": report.facts},
	}
}

func saGetRawReply(msg Message) (*spamc.SpamDOut, error) {
	body := make([]string, 0)

	for _, header := range msg.getHeaders() {
		body = append(body, (*header).getKey()+": "+(*header).getValue())
	}

	body = append(body, "")
	body = append(body, msg.getBody())

	host := Config.SpamAssassin.Host + ": " + strconv.Itoa(Config.SpamAssassin.Port)
	client := spamc.New(host, 10)
	return client.Report(strings.Join(body, "\r\n"), msg.getRecipients()[0])
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
