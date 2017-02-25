// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package greylisting

import (
	"fmt"
	"net"
	"time"

	"cluegetter/core"

	libspf2 "github.com/Freeaqingme/go-libspf2"
)

const ModuleName = "greylisting"

// A period (in days) of a month seems legit. And then we
// want to allow for cases like a news letter sent every
// first Monday of the month
const greylist_validity = 40

var greylistSpf2 = libspf2.NewClient()

type greylistVerdict struct {
	verdict string
	date    *time.Time
}

type module struct {
	*core.BaseModule
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
	return m.Config().Greylisting.Enabled
}

func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	if !msg.Session().Config().Greylisting.Enabled {
		return nil
	}

	ip := msg.Session().Ip

	whitelist := msg.Session().Config().Greylisting.Whitelist_Spf
	res, spfDomain, spfWhitelistErr := m.ipIsSpfWhitelisted(net.ParseIP(ip), done, whitelist)
	if res {
		m.Log().Debugf("Found %s in %s SPF record", ip, spfDomain)
		m.updateWhitelist(msg)
		return &core.MessageCheckResult{
			Module:          ModuleName,
			SuggestedAction: core.MessagePermit,
			Message:         "",
			Score:           1,
			Determinants: map[string]interface{}{
				"Found in SPF whitelist": "true",
				"SpfError":               spfWhitelistErr,
				"SpfDomain":              spfDomain,
			},
		}
	}

	if m.ipIsWhitelisted(&ip) {
		m.Log().Debugf("Found %s in greylist whitelist", ip)
		m.updateWhitelist(msg)
		return &core.MessageCheckResult{
			Module:          ModuleName,
			SuggestedAction: core.MessagePermit,
			Message:         "",
			Score:           1,
			Determinants: map[string]interface{}{
				"Found in whitelist":     "true",
				"Found in SPF whitelist": "false",
				"SpfError":               spfWhitelistErr,
				"SpfDomain":              spfDomain,
			},
		}
	}

	out := m.getVerdict(msg, spfWhitelistErr, spfDomain)
	if out != nil && out.SuggestedAction == core.MessagePermit {
		m.updateWhitelist(msg)
	}
	return out
}

func (m *module) getVerdict(msg *core.Message, spfWhitelistErr error, spfDomain string) *core.MessageCheckResult {
	determinants := map[string]interface{}{
		"Found in whitelist":     "false",
		"Found in SPF whitelist": "false",
		"SpfError":               spfWhitelistErr,
		"SpfDomain":              spfDomain,
		"Store":                  "redis",
	}

	sess := msg.Session()
	key := fmt.Sprintf("cluegetter-%d-greylisting-msg-%s_%s_%s", m.Instance(), sess.Ip, msg.From, msg.Rcpt)
	res, err := m.Redis().Get(key).Int64()
	if err == nil {
		determinants["time_diff"] = time.Now().Unix() - res
		if (res + (int64(sess.Config().Greylisting.Initial_Period) * 60)) < time.Now().Unix() {
			return &core.MessageCheckResult{
				Module:          ModuleName,
				SuggestedAction: core.MessagePermit,
				Message:         "",
				Score:           1,
				Determinants:    determinants,
			}
		}
	} else {
		m.Redis().Set(key, time.Now().Unix(), time.Duration(90)*time.Minute)
	}

	return &core.MessageCheckResult{
		Module:          ModuleName,
		SuggestedAction: core.MessageTempFail,
		Message:         "Greylisting in effect, please come back later",
		Score:           sess.Config().Greylisting.Initial_Score,
		Determinants:    determinants,
	}
}

func (m *module) ipIsWhitelisted(ip *string) bool {
	key := fmt.Sprintf("cluegetter-%d-greylisting-ip-%s", m.Instance(), *ip)
	return m.Redis().Exists(key).Val()
}

func (m *module) updateWhitelist(msg *core.Message) {
	key := fmt.Sprintf("cluegetter-%d-greylisting-ip-%s", m.Instance(), msg.Session().Ip)
	m.Redis().Set(key, time.Now().Unix(), greylist_validity*24*time.Hour)
}

func (m *module) ipIsSpfWhitelisted(ip net.IP, done chan bool, whitelist []string) (bool, string, error) {
	var error error
	for _, whitelistDomain := range whitelist {
		res, err := greylistSpf2.Query(whitelistDomain, ip)
		if err != nil {
			error = err
			m.Log().Errorf("Error while retrieving SPF for %s from %s: %s", ip, whitelistDomain, err)
			continue
		}

		if res == libspf2.SPFResultPASS {
			m.Log().Debugf("Got SPF result for %s from %s: %s", ip, whitelistDomain, res)
			return true, whitelistDomain, error
		}
	}

	return false, "", error
}
