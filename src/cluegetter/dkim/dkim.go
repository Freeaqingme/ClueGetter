// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package dkim

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"

	"cluegetter/core"
	fileBackend "cluegetter/dkim/backend/file"

	dkim "github.com/Freeaqingme/go-dkim"
)

const ModuleName = "dkim"

const (
	signing_required = "required"
	signing_optional = "optional"
	signing_none     = "none"
)

type module struct {
	*core.BaseModule

	backend backend
}

type backend interface {
	HasKey(*rsa.PublicKey) bool

	NewSigner(*dkim.PubKey) (dkim.Signer, error)
	GetPreferredKey([]*dkim.PubKey) *dkim.PubKey
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
	return m.Config().Dkim.Enabled
}

func (m *module) Init() {
	switch m.Config().Dkim.Backend {
	case "file":
		m.initFileBackend()
	default:
		m.Log().Fatalf("Invalid backend specified: %s", m.Config().Dkim.Backend)
	}
}

func (m *module) initFileBackend() {
	var err error
	m.backend, err = fileBackend.NewFileBackend(m.Config().Dkim_FileBackend.Key_Path)
	if err != nil {
		m.Log().Fatalf("Could not instantiate DKIM Key Store: %s", err.Error())
	}
}

func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	determinants := map[string]interface{}{
		"selectors": msg.Session().Config().Dkim.Selector,
	}
	errMsg := "An internal error has occurred"
	var hdr string

	// TODO: Check somewhere if domain and from/sender headers match
	// TODO: Check somewhere that from & subject header only occur once

	required, err := signingRequired(msg)
	determinants["sign"] = required
	if err != nil {
		goto Error
	}

	if required == signing_none {
		return &core.MessageCheckResult{
			Module:          m.Name(),
			SuggestedAction: core.MessagePermit,
			Message:         "",
			Score:           0,
			Determinants:    determinants,
		}
	}

	hdr, err = m.signMessage(msg)
	if err != nil {
		if _, ok := err.(*noSelectorInDnsFoundError); ok {
			if required == signing_optional {
				determinants["reason"] = "No valid DKIM records found"
				return &core.MessageCheckResult{
					Module:          m.Name(),
					SuggestedAction: core.MessagePermit,
					Message:         "",
					Score:           0,
					Determinants:    determinants,
				}
			}
			errMsg = fmt.Sprintf(
				"No valid DKIM records were found in the DNS configuration of your domain '%s'",
				msg.From.Domain(),
			)
			return &core.MessageCheckResult{
				Module:          m.Name(),
				SuggestedAction: core.MessageReject,
				Message:         errMsg,
				Score:           100,
				Determinants:    determinants,
			}
		}

		goto Error
	}

	determinants["injectedHeader"] = []string{hdr}
	return &core.MessageCheckResult{
		Module:          m.Name(),
		SuggestedAction: core.MessagePermit,
		Message:         "",
		Score:           0,
		Determinants:    determinants,
	}

Error:
	determinants["error"] = err.Error()
	return &core.MessageCheckResult{
		Module:          m.Name(),
		SuggestedAction: core.MessageError,
		Message:         errMsg,
		Score:           25,
		Determinants:    determinants,
	}
}

func (m *module) signMessage(msg *core.Message) (string, error) {
	domain := msg.From.Domain()

	dkimKeys := m.getDkimKeys(msg)
	if len(dkimKeys) == 0 {
		return "", &noSelectorInDnsFoundError{
			msg: "No selectors found in DNS",
		}
	}
	dkimKey := m.backend.GetPreferredKey(dkimKeys)
	if dkimKey == nil {
		return "", &noSelectorInDnsFoundError{
			msg: "No selectors found in DNS",
		}
	}

	conf := m.Config().Dkim
	dkim := dkim.NewDkim()
	options := dkim.NewSigOptions()
	options.Domain = domain
	options.Selector = dkimKey.Selector
	//options.SignatureExpireIn = 3600
	options.BodyLength = conf.Sign_Bodylength
	options.Headers = conf.Sign_Headers
	options.Canonicalization = "relaxed/relaxed"

	signer, err := m.backend.NewSigner(dkimKey)
	if err != nil {
		return "", err
	}

	dHeader, err := dkim.GetDkimHeader(msg.String(), signer, &options)
	if err != nil {
		return "", err
	}

	msg.AddHeader(core.MessageHeader{
		Key:   "DKIM-Signature",
		Value: dHeader,
	})

	return dHeader, nil
}

func (m *module) getDkimKeys(msg *core.Message) []*dkim.PubKey {
	recordsFound := make([]*dkim.PubKey, 0)
	dkim := dkim.NewDkim()

	domain := msg.From.Domain()
	for _, selector := range msg.Session().Config().Dkim.Selector {
		res, err := dkim.PubKeyFromDns(selector, domain)
		if err != nil {
			m.Log().Debug("Could not get DKIM record '%s._domainkey.%s': %s", selector, domain, err.Error())
			continue
		}

		recordsFound = append(recordsFound, res...)
	}

	return recordsFound
}

func signingRequired(msg *core.Message) (string, error) {
	mode := strings.ToLower(msg.Session().Config().Dkim.Sign)

	switch mode {
	case "required":
		return signing_required, nil
	case "optional":
		return signing_optional, nil
	case "none":
		return signing_none, nil
	}

	return "", errors.New("Invalid signing mode: " + mode)
}

type noSelectorInDnsFoundError struct {
	msg string
}

func (err noSelectorInDnsFoundError) Error() string {
	return err.msg
}
