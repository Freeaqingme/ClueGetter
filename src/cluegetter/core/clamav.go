// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
// Test using: http://sanesecurity.com/support/signature-testing/
//
package core

import (
	"bytes"
	"time"

	clamd "github.com/Freeaqingme/go-clamd"
)

var (
	clamdClient *clamd.Clamd
)

func init() {
	enable := func() bool { return Config.Clamav.Enabled }
	init := clamavInit
	milterCheck := clamavMilterCheck

	ModuleRegister(&ModuleOld{
		name:        "clamav",
		enable:      &enable,
		init:        &init,
		milterCheck: &milterCheck,
	})
}

func clamavInit() {
	clamdClient = clamd.NewClamd(Config.Clamav.Address)

	err := clamdClient.Ping()
	if err != nil {
		Log.Fatalf("Could not connect to Clamav: %s", err.Error())
	}

	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ticker.C:
				go func() {
					CluegetterRecover("clamavDumpStats")
					clamavDumpStats()
				}()
			}
		}
	}()
}

func clamavDumpStats() {
	stats, err := clamdClient.Stats()
	if err != nil {
		Log.Errorf("Error while dumping stats from ClamAV: %s", err.Error())
	}

	Log.Infof("ClamAV stats: %v", stats)
}

func clamavMilterCheck(msg *Message, abort chan bool) *MessageCheckResult {
	if !msg.session.config.Clamav.Enabled {
		return nil
	}

	sconf := msg.session.config.Clamav
	msgStr := msg.String()
	if len(msgStr) > sconf.Max_Size {
		msgStr = msgStr[:sconf.Max_Size]
	}

	res, err := clamdClient.ScanStream(bytes.NewReader(msgStr), abort)
	if err != nil {
		Log.Errorf("Problem while talking to Clamd while checking for %s: %s", msg.QueueId, err.Error())
		return &MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: MessageError,
			Message:         "An internal error occurred.",
			Score:           25,
			Determinants:    map[string]interface{}{"error": err.Error()},
		}
	}

	// There really should only be 1 item in res, but just in case there's
	// more, we do want to close everything.
	defer func() {
		for v := range res {
			Log.Noticef("Got an additional ClamAV result, but it was discarded while scanning %s: %s",
				msg.QueueId, v.Raw)
		}
	}()

	v := <-res
	if v == nil {
		Log.Infof("clamavMilterCheck(): No result received over result cannel. Channel closed?")
		return nil
	}

	switch v.Status {
	case clamd.RES_OK:
		return &MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: MessageReject,
			Message:         "",
			Score:           0,
			Determinants:    clamavGetDeterminants(v),
		}
	case clamd.RES_FOUND:
		return &MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: MessageReject,
			Message: "Our system has detected that this message appears to contain malicious or " +
				"otherwise harmful content. Therefore, this message has been blocked.",
			Score:        msg.session.config.Clamav.Default_Score,
			Determinants: clamavGetDeterminants(v),
		}
	case clamd.RES_PARSE_ERROR:
		Log.Errorf("Could not parse output from Clamd while checking for %s. Raw output: %s",
			msg.QueueId, v.Raw)
		return &MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: MessageError,
			Message:         "An internal error occurred.",
			Score:           25,
			Determinants:    clamavGetDeterminants(v),
		}
	case clamd.RES_ERROR:
		Log.Errorf("Clamd returned an error while checking for %s: %s", msg.QueueId, v.Description)

		return &MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: MessageError,
			Message:         "An internal error occurred.",
			Score:           25,
			Determinants:    clamavGetDeterminants(v),
		}
	default:
		Log.Errorf("Clamd returned an unrecognized status code. This shouldn't happen! "+
			"While checking for %s: %s", msg.QueueId, v.Status)

		return &MessageCheckResult{
			Module:          "clamav",
			SuggestedAction: MessageError,
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
