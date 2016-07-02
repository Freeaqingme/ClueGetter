// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package spamassassin

import (
	"fmt"
	spamc "github.com/Freeaqingme/go-spamc"
	"strconv"
	"strings"

	"cluegetter/core"
)

const ModuleName = "spamassassin"

type module struct {
	*core.BaseModule

	cg *core.Cluegetter
}

func init() {
	core.ModuleRegister(&module{})
}

func (m *module) Name() string {
	return ModuleName
}

func (m *module) SetCluegetter(cg *core.Cluegetter) {
	m.cg = cg
}

func (m *module) Enable() bool {
	return m.cg.Config.SpamAssassin.Enabled
}

type saReport struct {
	score float64
	facts []*saReportFact
}

type saReportFact struct {
	Score       float64
	Symbol      string
	Description string
}

func (m *module) MessageCheck(msg *core.Message, abort chan bool) *core.MessageCheckResult {
	if !msg.Session().Config().SpamAssassin.Enabled {
		return nil
	}

	rawReply, err := m.getRawReply(msg, abort)
	if err != nil || rawReply.Code != spamc.EX_OK {
		m.cg.Log.Errorf("SpamAssassin returned an error: %s", err)
		return &core.MessageCheckResult{
			Module:          ModuleName,
			SuggestedAction: core.MessageError,
			Message:         "An internal error occurred",
			Score:           25,
			Determinants:    map[string]interface{}{"error": err.Error()},
		}
	}

	m.cg.Log.Debugf("Getting SA report for %s", msg.QueueId)
	report := saParseReply(rawReply)
	factsStr := func() []string {
		out := make([]string, 0)
		for _, fact := range report.facts {
			out = append(out, fmt.Sprintf("%s=%.3f", fact.Symbol, fact.Score))
		}
		return out
	}()

	m.cg.Log.Debugf("Got SA score of %.2f for %s. Tests: [%s]", report.score, msg.QueueId, strings.Join(factsStr, ","))
	return &core.MessageCheckResult{
		Module:          "spamassassin",
		SuggestedAction: core.MessageReject,
		Message: "Our system has detected that this message is likely unsolicited mail (SPAM). " +
			"To reduce the amount of spam, this message has been blocked.",
		Score:        report.score,
		Determinants: map[string]interface{}{"report": report.facts},
	}
}

func (m *module) getRawReply(msg *core.Message, abort chan bool) (*spamc.SpamDOut, error) {
	bodyStr := string(msg.String())

	host := m.cg.Config.SpamAssassin.Host + ":" + strconv.Itoa(m.cg.Config.SpamAssassin.Port)
	sconf := msg.Session().Config().SpamAssassin
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

func (m *module) BayesLearn(msg *core.Message, isSpam bool) {
	bodyStr := string(msg.String())

	host := m.cg.Config.SpamAssassin.Host + ":" + strconv.Itoa(m.cg.Config.SpamAssassin.Port)
	sconf := msg.Session().Config().SpamAssassin
	client := spamc.New(host, sconf.Timeout, sconf.Connect_Timeout)

	abort := make(chan bool) // unused really. To implement or not to implement. That's the question
	if isSpam {
		client.Learn(abort, spamc.LEARN_SPAM, bodyStr)
	} else {
		client.Learn(abort, spamc.LEARN_HAM, bodyStr)
	}
	close(abort)
}
