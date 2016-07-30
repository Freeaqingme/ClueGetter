// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
// Test using: http://sanesecurity.com/support/signature-testing/
//
package clamav

import (
	"bytes"
	"time"

	"cluegetter/core"

	clamd "github.com/Freeaqingme/go-clamd"
)

const ModuleName = "clamav"

type module struct {
	*core.BaseModule

	client *clamd.Clamd
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
	return m.Config().Clamav.Enabled
}

func (m *module) Init() {
	m.client = clamd.NewClamd(m.Config().Clamav.Address)

	err := m.client.Ping()
	if err != nil {
		m.Log().Fatalf("Could not connect to Clamav: %s", err.Error())
	}

	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ticker.C:
				go func() {
					core.CluegetterRecover("clamavDumpStats")
					m.clamavDumpStats()
				}()
			}
		}
	}()
}

func (m *module) clamavDumpStats() {
	stats, err := m.client.Stats()
	if err != nil {
		m.Log().Errorf("Error while dumping stats from ClamAV: %s", err.Error())
	}

	m.Log().Infof("ClamAV stats: %v", stats)
}

func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	sconf := msg.Session().Config().Clamav

	if !sconf.Enabled {
		return nil
	}

	msgStr := msg.String()
	if len(msgStr) > sconf.Max_Size {
		msgStr = msgStr[:sconf.Max_Size]
	}

	res, err := m.client.ScanStream(bytes.NewReader(msgStr), done)
	if err != nil {
		m.Log().Errorf("Problem while talking to Clamd while checking for %s: %s", msg.QueueId, err.Error())
		return &core.MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: core.MessageError,
			Message:         "An internal error occurred.",
			Score:           25,
			Determinants:    map[string]interface{}{"error": err.Error()},
		}
	}

	// There really should only be 1 item in res, but just in case there's
	// more, we do want to close everything.
	defer func() {
		for v := range res {
			m.Log().Noticef("Got an additional ClamAV result, but it was discarded while scanning %s: %s",
				msg.QueueId, v.Raw)
		}
	}()

	v := <-res
	if v == nil {
		m.Log().Infof("clamavMilterCheck(): No result received over result cannel. Channel closed?")
		return nil
	}

	switch v.Status {
	case clamd.RES_OK:
		return &core.MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: core.MessageReject,
			Message:         "",
			Score:           0,
			Determinants:    clamavGetDeterminants(v),
		}
	case clamd.RES_FOUND:
		return &core.MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: core.MessageReject,
			Message: "Our system has detected that this message appears to contain malicious or " +
				"otherwise harmful content. Therefore, this message has been blocked.",
			Score:        msg.Session().Config().Clamav.Default_Score,
			Determinants: clamavGetDeterminants(v),
		}
	case clamd.RES_PARSE_ERROR:
		m.Log().Errorf("Could not parse output from Clamd while checking for %s. Raw output: %s",
			msg.QueueId, v.Raw)
		return &core.MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: core.MessageError,
			Message:         "An internal error occurred.",
			Score:           25,
			Determinants:    clamavGetDeterminants(v),
		}
	case clamd.RES_ERROR:
		m.Log().Errorf("Clamd returned an error while checking for %s: %s", msg.QueueId, v.Description)

		return &core.MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: core.MessageError,
			Message:         "An internal error occurred.",
			Score:           25,
			Determinants:    clamavGetDeterminants(v),
		}
	default:
		m.Log().Errorf("Clamd returned an unrecognized status code. This shouldn't happen! "+
			"While checking for %s: %s", msg.QueueId, v.Status)

		return &core.MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: core.MessageError,
			Message:         "An internal error occurred.",
			Score:           25,
			Determinants:    clamavGetDeterminants(v),
		}
	}

}

func clamavGetDeterminants(res *clamd.ScanResult) map[string]interface{} {
	return map[string]interface{}{
		"raw":         res.Raw,
		"description": res.Description,
		"path":        res.Path,
		"hash":        res.Hash,
		"size":        res.Size,
		"status":      res.Status,
	}
}
