// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"fmt"
	spamc "github.com/Freeaqingme/go-spamc"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
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

func saStart() {
	if Config.SpamAssassin.Enabled != true {
		Log.Info("Skipping SpamAssassin module because it was not enabled in the config")
		return
	}

	Log.Info("SpamAssassin module started successfully")
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

func saBuildInputMessage(msg Message) []string {
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

	// Let SA know where the email came from.
	body = append(body, fmt.Sprintf("Received: from %s (%s [%s])\r\n\tby %s with SMTP id %d@%s; %s",
		sess.getHelo(),
		revdnsStr,
		sess.getIp(),
		fqdn,
		sess.getId(),
		fqdn,
		time.Now().Format(time.RFC1123Z)))

	for _, header := range msg.getHeaders() {
		body = append(body, (*header).getKey()+": "+(*header).getValue())
	}

	body = append(body, "")
	for _, bodyChunk := range msg.getBody() {
		body = append(body, bodyChunk)
	}

	return body
}

func saGetRawReply(msg Message) (*spamc.SpamDOut, error) {
	body := saBuildInputMessage(msg)

	host := Config.SpamAssassin.Host + ":" + strconv.Itoa(Config.SpamAssassin.Port)
	client := spamc.New(host, 10)
	return client.Report(strings.Join(body, "\r\n")[:Config.SpamAssassin.Max_Size], msg.getRecipients()[0])
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
