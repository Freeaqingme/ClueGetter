// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package dkim

import (
	"crypto/rsa"
	//	"fmt"
	"errors"

	"cluegetter/core"
	fileBackend "cluegetter/dkim/backend/file"

	dkim "github.com/Freeaqingme/go-dkim"
)

const ModuleName = "dkim"

type module struct {
	*core.BaseModule

	cg      *core.Cluegetter
	backend backend
}

type backend interface {
	HasKey(*rsa.PublicKey) bool

	NewSigner(*dkim.PubKey) (dkim.Signer, error)
	GetPreferredKey([]*dkim.PubKey) *dkim.PubKey
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
	switch m.cg.Config.Dkim.Backend {
	case "file":
		m.initFileBackend()
	default:
		m.cg.Log.Fatalf("Invalid backend specified: %s", m.cg.Config.Dkim.Backend)

	}
}

func (m *module) initFileBackend() {
	var err error
	m.backend, err = fileBackend.NewFileBackend(m.cg.Config.Dkim_FileBackend.Key_Path)
	if err != nil {
		m.cg.Log.Fatalf("Could not instantiate DKIM Key Store: %s", err.Error())
	}
}

func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {

	// TODO: Check somewhere if domain and from/sender headers match
	// TODO: Check somewhere that from & subject header only occur once

	err := m.signMessage(msg)
	if err != nil {
		m.cg.Log.Errorf("DKIM: %s", err.Error())
		return &core.MessageCheckResult{
			Module:          m.Name(),
			SuggestedAction: core.MessageError,
			Message:         "An internal error occurred.",
			Score:           25,
			Determinants:    make(map[string]interface{}, 0),
		}
	}

	return nil
}

func (m *module) signMessage(msg *core.Message) error {
	domain := msg.From.Domain()

	dkimKeys := m.getDkimKeys(domain)
	dkimKey := m.backend.GetPreferredKey(dkimKeys)
	if dkimKey == nil {
		return errors.New("No valid key could be found for " + domain)
	}

	dkim := dkim.NewDkim()
	options := dkim.NewSigOptions()
	options.Domain = domain
	options.Selector = dkimKey.Selector
	//options.SignatureExpireIn = 3600
	options.SignatureExpireIn = 0
	options.BodyLength = 50
	options.Headers = []string{"from", "subject"}
	//options.AddSignatureTimestamp = true
	options.AddSignatureTimestamp = false
	options.Canonicalization = "relaxed/relaxed"

	signer, err := m.backend.NewSigner(dkimKey)
	if err != nil {
		return err
	}

	dHeader, err := dkim.GetDkimHeader(msg.String(), signer, &options)
	if err != nil {
		return err
	}

	msg.AddHeader(core.MessageHeader{
		Key:   "DKIM-Signature",
		Value: dHeader,
	})

	return nil
}

func (m *module) getDkimKeys(domain string) []*dkim.PubKey {
	recordsFound := make([]*dkim.PubKey, 0)
	dkim := dkim.NewDkim()

	for _, selector := range m.cg.Config.Dkim.Selector {
		res, err := dkim.PubKeyFromDns(selector, domain)
		if err != nil {
			m.cg.Log.Notice("Could not get DKIM record '%s._domainkey.%s': %s", selector, domain, err.Error())
			continue
		}

		recordsFound = append(recordsFound, res...)
	}

	return recordsFound
}
