// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package dkim

import (
	"cluegetter/core"

	dkim "github.com/Freeaqingme/go-dkim"
	"strings"
	"net"
	"errors"
	"fmt"

	//"cluegetter/address"

	"github.com/miekg/dns"
)

const ModuleName = "dkim"

type module struct {
	*core.BaseModule

	cg     *core.Cluegetter
	dnsClient *dns.Client
	dnsConf *dns.ClientConfig
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
	return m.cg.Config.Dkim.Enabled
}

func (m *module) Init() {
	var err error
	m.dnsConf, err = dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		m.cg.Log.Fatalf("Could not instantiate DNS Config: %s", err.Error)
	}

	m.dnsClient = &dns.Client{}
}

func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	domain := msg.From.Domain()

/*
	for _, hdr := range msg.Headers {
		if strings.EqualFold(hdr.Key, "from") || strings.EqualFold(hdr.Key, "sender") {
			hdrFrom := address.FromString(hdr.Value)
			if ! strings.EqualFold(hdrFrom.Domain(), domain) {
				// Envelope From and message From do not match. Do something now?
			}
		}
	}*/

	dkimKeys := m.getDkimKeys(domain)
	dkimKeys = m.filterValidDkimKeys(dkimKeys)

	fmt.Println(dkimKeys[0])
	return nil
}

func (m *module) getDkimKeys(domain string) []*dkim.PubKey {
	recordsFound := make([]*dkim.PubKey, 0)
	for _, selector := range m.cg.Config.Dkim.Selector {
		res, _, err := dkim.PubKeyFromDns(selector, domain)
		if err != nil {
			m.cg.Log.Notice("Could not get DKIM record '%s._domainkey.%s': %s", selector, domain, err.Error())
			continue
		}

		recordsFound = append(recordsFound, res...)
	}

	return recordsFound
}

func (m *module) filterValidDkimKeys([]*dkim.PubKey) []*dkim.PubKey {
	out := make([]*dkim.PubKey, 0)

	// todo

	return out
}

func (m *module) getDkimRecord(host string) ([]string, error) {
	dnsMsg:= &dns.Msg{}
	dnsMsg.SetQuestion(dns.Fqdn(host), dns.TypeTXT)
	dnsMsg.RecursionDesired = true

	r, _, err := m.dnsClient.Exchange(dnsMsg, net.JoinHostPort(m.dnsConf.Servers[0], m.dnsConf.Port))
	if r == nil {
		return nil, err
	}

	if r.Rcode != dns.RcodeSuccess {
		return nil, errors.New(fmt.Sprintf("Received code %d", r.Rcode))
	}

	out := make([]string, 0)
	for _, a := range r.Answer {
		if a.Header().Rrtype != dns.TypeTXT {
			continue
		}

		txtRecord := a.(*dns.TXT)
		out = append(out, strings.Join(txtRecord.Txt, "")) // probably should use dns.sprintTxt() ?!?
	}

	return out,nil
}
