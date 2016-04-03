// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
// Test using: http://sanesecurity.com/support/signature-testing/
//
package main

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

	ModuleRegister(&module{
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
		Log.Fatal("Could not connect to Clamav: %s", err.Error())
	}

	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ticker.C:
				go func() {
					cluegetterRecover("clamavDumpStats")
					clamavDumpStats()
				}()
			}
		}
	}()
}

func clamavDumpStats() {
	stats, err := clamdClient.Stats()
	if err != nil {
		Log.Error("Error while dumping stats from ClamAV: %s", err.Error())
	}

	Log.Info("ClamAV stats: %v", stats)
}

func clamavMilterCheck(msg *Message, abort chan bool) *MessageCheckResult {
	if !Config.Clamav.Enabled || !msg.session.config.Clamav.Enabled {
		return nil
	}

	res, err := clamdClient.ScanStream(bytes.NewReader(msg.String()), abort)
	if err != nil {
		Log.Error("Problem while talking to Clamd while checking for %s: %s", msg.QueueId, err.Error())
		return &MessageCheckResult{
			module:          "clamav",
			suggestedAction: messageError,
			message:         "An internal error occurred.",
			score:           25,
			determinants:    map[string]interface{}{"error": err.Error()},
		}
	}

	// There really should only be 1 item in res, but just in case there's
	// more, we do want to close everything.
	defer func() {
		for _ = range res {
		}
	}()

	v := <-res
	if v == nil {
		Log.Info("clamavMilterCheck(): No result received over result cannel. Channel closed?")
		return nil
	}

	switch v.Status {
	case clamd.RES_OK:
		return &MessageCheckResult{
			module:          "clamav",
			suggestedAction: messageReject,
			message:         "",
			score:           0,
			determinants:    clamavGetDeterminants(v),
		}
	case clamd.RES_FOUND:
		return &MessageCheckResult{
			module:          "clamav",
			suggestedAction: messageReject,
			message: "Our system has detected that this message appears to contain malicious or " +
				"otherwise harmful content. Therefore, this message has been blocked.",
			score:        msg.session.config.Clamav.Default_Score,
			determinants: clamavGetDeterminants(v),
		}
	case clamd.RES_PARSE_ERROR:
		Log.Error("Could not parse output from Clamd while checking for %s. Raw output: %s",
			msg.QueueId, v.Raw)
		return &MessageCheckResult{
			module:          "clamav",
			suggestedAction: messageError,
			message:         "An internal error occurred.",
			score:           25,
			determinants:    clamavGetDeterminants(v),
		}
	case clamd.RES_ERROR:
		Log.Error("Clamd returned an error while checking for %s: %s", msg.QueueId, v.Description)

		return &MessageCheckResult{
			module:          "clamav",
			suggestedAction: messageError,
			message:         "An internal error occurred.",
			score:           25,
			determinants:    clamavGetDeterminants(v),
		}
	default:
		Log.Error("Clamd returned an unrecognized status code. This shouldn't happen! "+
			"While checking for %s: %s", msg.QueueId, v.Status)

		return &MessageCheckResult{
			module:          "clamav",
			suggestedAction: messageError,
			message:         "An internal error occurred.",
			score:           25,
			determinants:    clamavGetDeterminants(v),
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
