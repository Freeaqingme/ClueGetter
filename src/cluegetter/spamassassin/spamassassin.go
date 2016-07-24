// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package spamassassin

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"cluegetter/core"

	spamc "github.com/Freeaqingme/go-spamc"
)

const ModuleName = "spamassassin"

type module struct {
	*core.BaseModule

	verdictMsgs map[string]string
}

func init() {
	core.ModuleRegister(&module{
		BaseModule: core.NewBaseModule(nil),
	})
}

func (m *module) Name() string {
	return ModuleName
}

func (m *module) Enable() bool {
	return m.Config().SpamAssassin.Enabled
}

func (m *module) Init() {
	m.verdictMsgs = make(map[string]string, 0)

	for _, msg := range m.Config().SpamAssassin.Verdict_Msg {
		split := strings.SplitN(msg, ":", 2)
		if len(split) < 2 {
			m.Log().Fatalf("%s: Verdict message does not fit format '<key>: <message>': %s", ModuleName, msg)
		}

		key := strings.ToUpper(split[0])
		if _, set := m.verdictMsgs[split[0]]; set {
			m.Log().Fatalf("%s: A verdict message for key '%s' was configured more than once", ModuleName, key)
		}

		m.verdictMsgs[key] = strings.TrimSpace(trimAbundantSpace(split[1]))
	}

}

func (m *module) MessageCheck(msg *core.Message, abort chan bool) *core.MessageCheckResult {
	if !msg.Session().Config().SpamAssassin.Enabled {
		return nil
	}

	m.Log().Debugf("Getting SA report for %s", msg.QueueId)
	report, err := m.scan(msg, abort)
	if err != nil {
		m.Log().Errorf("SpamAssassin returned an error: %s", err)
		return &core.MessageCheckResult{
			Module:          ModuleName,
			SuggestedAction: core.MessageError,
			Message:         "An internal error occurred",
			Score:           25,
			Determinants:    map[string]interface{}{"error": err.Error()},
		}
	}

	m.Log().Debugf("Got SA score of %.2f for %s. Tests: [%s]",
		report.score, msg.QueueId, strings.Join(report.factsAsString(), ","),
	)

	sort.Sort(report.facts) // Put facts with highest score on top
	return &core.MessageCheckResult{
		Module:          "spamassassin",
		SuggestedAction: core.MessageReject,
		Message:         report.verdictMessage(),
		Score:           report.score,
		Determinants: map[string]interface{}{
			"report": report.facts,
		},
	}
}

func (m *module) getRawReply(msg *core.Message, abort chan bool) (*spamc.SpamDOut, error) {
	bodyStr := string(msg.String())

	host := m.Config().SpamAssassin.Host + ":" + strconv.Itoa(m.Config().SpamAssassin.Port)
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
func (m *module) scan(msg *core.Message, abort chan bool) (*report, error) {
	reply, err := m.getRawReply(msg, abort)
	if err != nil {
		return nil, err
	}
	if reply.Code != spamc.EX_OK {
		return nil, errors.New("reply.code was: " + strconv.Itoa(reply.Code))
	}

	report := &report{module: m, facts: make([]reportFact, 0)}

	for key, value := range reply.Vars {
		if key == "spamScore" {
			report.score = value.(float64)
		} else if key == "report" {
			var reportFacts []map[string]interface{}
			reportFacts = value.([]map[string]interface{})
			for _, reportFact := range reportFacts {
				report.facts = append(report.facts, m.saParseReplyReportVar(reportFact))
			}
		}
	}

	return report, nil
}

func (m *module) saParseReplyReportVar(reportFactRaw map[string]interface{}) reportFact {
	reportFact := reportFact{}
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

// See: http://intogooglego.blogspot.nl/2015/05/day-6-string-minifier-remove-whitespaces.html
func trimAbundantSpace(in string) (out string) {
	white := false
	for _, c := range in {
		if unicode.IsSpace(c) {
			if !white {
				out = out + " "
			}
			white = true
		} else {
			out = out + string(c)
			white = false
		}
	}
	return
}
