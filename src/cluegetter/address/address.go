// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package address

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/weppos/publicsuffix-go/publicsuffix"
)

type Address struct {
	local  string
	domain string

	sld   string // Second Level Domain
	sldMu sync.RWMutex
}

func (a *Address) Local() string {
	return a.local
}

func (a *Address) Domain() string {
	return a.domain
}

// Second Level Domain
func (a *Address) Sld() string {
	a.sldMu.RLock()

	if a.sld != "" {
		a.sldMu.RUnlock()
		return a.sld
	}

	if a.Domain() == "" {
		a.sldMu.RUnlock()
		return ""
	}

	a.sldMu.RUnlock()
	a.sldMu.Lock() // Acquire RWLock instead of RLock
	defer a.sldMu.Unlock()

	a.sld, _ = publicsuffix.DomainFromListWithOptions(
		publicsuffix.DefaultList,
		a.Domain(),
		&publicsuffix.FindOptions{IgnorePrivate: true},
	)

	return a.sld
}

func (a *Address) String() string {
	if a.domain == "" {
		return a.local
	}

	return a.local + "@" + a.domain
}

func (a *Address) MarshalJSON() ([]byte, error) {
	type Alias Address
	return json.Marshal(&struct {
		Local  string
		Domain string
		Sld    string
	}{
		a.Local(),
		a.Domain(),
		a.Sld(),
	})
}

func FromString(address string) *Address {
	a := &Address{}
	a.local, a.domain = messageParseAddress(address, true)

	return a
}

func FromAddressOrDomain(address string) *Address {
	a := &Address{}
	a.local, a.domain = messageParseAddress(address, false)

	return a
}

func messageParseAddress(address string, singleIsUser bool) (local, domain string) {
	if strings.Index(address, "@") != -1 {
		local = strings.SplitN(address, "@", 2)[0]
		domain = strings.SplitN(address, "@", 2)[1]
	} else if singleIsUser {
		local = address
		domain = ""
	} else {
		local = ""
		domain = address
	}

	return
}
