// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package srs

import (
	"fmt"
	"strings"

	"cluegetter/address"
	"cluegetter/core"
	"regexp"
	"time"
)

const ModuleName = "srs"

var srsMatch = regexp.MustCompile(`^(?i)SRS[0-9]+=`)

type srsModule struct {
	cg *core.Cluegetter
}

func init() {
	core.ModuleRegister(&srsModule{})
}

func (m *srsModule) Name() string {
	return ModuleName
}

func (m *srsModule) SetCluegetter(cg *core.Cluegetter) {
	m.cg = cg
}

func (m *srsModule) Enable() bool {
	return m.cg.Config.Srs.Enabled
}

func (m *srsModule) Stop() {}

func (m *srsModule) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	from := ""
	srsIn := m.srsGetInboundSrsAddresses(msg)

	if len(srsIn) > 0 && len(msg.Rcpt) > 1 {
		m.cg.Log.Notice("More than 1 recipient including an SRS recipient, that's weird?")
	}

	var mapped map[string]string
	if len(srsIn) > 0 {
		mapped = m.srsSwapRecipients(msg, srsIn)
	} else {
		from = m.srsGetFromAddress(msg)
		core.MilterChangeFrom(msg.Session(), from)
		go func() {
			core.CluegetterRecover("srsPersist")
			m.srsPersist(msg, from)
		}()
	}

	return &core.MessageCheckResult{
		Module:          ModuleName,
		SuggestedAction: core.MessagePermit,
		Score:           0,
		Determinants: map[string]interface{}{
			"from":         from,
			"is-forwarded": m.srsIsForwarded(msg),
			"mapped":       mapped,
		},
	}
}

func (m *srsModule) RecipientCheck(rcpt *address.Address) (verdict int, msg string) {
	if !m.srsIsSrsAddress(rcpt) {
		return core.MessagePermit, ""
	}

	if m.srsLookupAddress(rcpt) == "" {
		return core.MessageReject, ""
	}

	return core.MessagePermit, ""
}

func (m *srsModule) srsSwapRecipients(msg *core.Message, srsAddresses []address.Address) map[string]string {
	out := make(map[string]string, 0)
	for _, srsAddress := range srsAddresses {
		out[srsAddress.String()] = m.srsLookupAddress(&srsAddress)

		core.MilterDelRcpt(msg.Session(), srsAddress.String())
		core.MilterAddRcpt(msg.Session(), out[srsAddress.String()])
	}

	return out
}

func (m *srsModule) srsLookupAddress(address *address.Address) string {
	key := strings.ToLower(fmt.Sprintf("cluegetter--srs-entry-%s", address.String()))
	out, _ := m.cg.Redis.Get(key).Result()
	return out
}

// Todo: Also persist in DB?
func (m *srsModule) srsPersist(msg *core.Message, from string) {
	key := strings.ToLower(fmt.Sprintf("cluegetter--srs-entry-%s", from))
	m.cg.Redis.Set(key, msg.From.String(), 7*24*time.Hour)
}

func (m *srsModule) srsIsSrsAddress(address *address.Address) bool {
	if !m.Enable() {
		return false // If SRS is not enabled, nothing is an SRS address
	}

	return srsMatch.MatchString(address.Local())
}

func (m *srsModule) srsGetInboundSrsAddresses(msg *core.Message) []address.Address {
	out := make([]address.Address, 0)
	for _, rcpt := range msg.Rcpt {
		if m.srsIsSrsAddress(rcpt) {
			out = append(out, *rcpt)
		}
	}

	return out
}

func (m *srsModule) srsGetFromAddress(msg *core.Message) string {
	if !m.Enable() {
		return ""
	}

	if !m.srsIsForwarded(msg) {
		return ""
	}

	domain := m.srsGetRewriteDomain(msg)
	if domain == "" {
		m.cg.Log.Debug("Could not determine SRS domain for %s", msg.QueueId)
		return ""
	}

	return fmt.Sprintf("SRS0=%s=%s=%s@%s",
		msg.QueueId, msg.From.Domain(), msg.From.Local(), domain)
}

func (m *srsModule) srsGetRewriteDomain(msg *core.Message) string {
	domains := make([]string, 0)
	for _, hdr := range msg.Headers {
		if strings.EqualFold(hdr.Key, m.cg.Config.Srs.Recipient_Header) {
			address := address.FromString(strings.ToLower(hdr.Value))
			domains = append(domains, address.Domain())
		}
	}

	for _, rcpt := range msg.Rcpt {
		rcptDomain := strings.ToLower(rcpt.Domain())
		for k, domain := range domains {
			if rcptDomain == domain {
				domains = append(domains[:k], domains[k+1:]...)
			}
		}
	}

	if len(domains) > 1 {
		m.cg.Log.Debug("Multiple SRS domains to choose from for message '%s': %s",
			msg.QueueId, domains,
		)
	}

	if len(domains) > 0 {
		return domains[0]
	}

	return ""
}

// Checks if the message was forwarded by comparing the recipient list
// to the Config.Srs.Recipient_Header headers. If a recipient does not show in the
// headers, it's safe to say the message was forwarded
func (m *srsModule) srsIsForwarded(msg *core.Message) bool {
	for _, rcpt := range msg.Rcpt {

		match := false
		count := 0
		for _, hdr := range msg.Headers {
			if strings.EqualFold(hdr.Key, m.cg.Config.Srs.Recipient_Header) {
				count++
				if strings.EqualFold(hdr.Value, rcpt.String()) {

					match = true
					break
				}
			}
		}

		if count == 0 { // No Config.Srs.Recipient_Header headers
			return false
		}

		if !match {
			return true
		}
	}

	return false
}

// Satisfy Interface
func (m *srsModule) Init() {
}

func (m *srsModule) Ipc() map[string]func(string) {
	return make(map[string]func(string), 0)
}

func (m *srsModule) Rpc() map[string]chan string {
	return make(map[string]chan string, 0)
}

func (m *srsModule) HttpHandlers() map[string]core.HttpCallback {
	return make(map[string]core.HttpCallback, 0)
}
