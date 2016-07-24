// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package file

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/ioutil"
	"path/filepath"
	"sync"

	dkim "github.com/Freeaqingme/go-dkim"
)

type backend struct {
	path []string

	*sync.RWMutex
	keys map[string]*rsa.PrivateKey
}

func NewFileBackend(path []string) (*backend, error) {
	backend := &backend{
		path:    path,
		RWMutex: &sync.RWMutex{},
	}

	err := backend.init()
	return backend, err
}

func (b *backend) HasKey(key *rsa.PublicKey) bool {
	b.RLock()
	defer b.RUnlock()

	_, ok := b.keys[key.N.String()]
	return ok
}

func (b *backend) GetPreferredKey(keys []*dkim.PubKey) *dkim.PubKey {
	for _, key := range keys {
		if b.HasKey(&key.PubKey) {
			return key
		}
	}

	return nil
}

func (b *backend) init() error {
	if err := b.loadKeys(); err != nil {
		return err
	}

	return nil
}

func (b *backend) getPrivKey(pubkey *rsa.PublicKey) *rsa.PrivateKey {
	b.RLock()
	defer b.RUnlock()

	privkey, _ := b.keys[pubkey.N.String()]
	return privkey
}

func (b *backend) loadKeys() error {
	keyFiles := make([]string, 0)
	for _, path := range b.path {
		files, err := filepath.Glob(path)
		if err != nil {
			return err
		}
		keyFiles = append(keyFiles, files...)
	}

	if len(keyFiles) == 0 {
		return errors.New("No .key files were found")
	}

	keys := make(map[string]*rsa.PrivateKey, 0)
	for _, keyFile := range keyFiles {
		keyContents, err := ioutil.ReadFile(keyFile)
		if err != nil {
			return err
		}

		block, _ := pem.Decode(keyContents)
		if block == nil {
			return errors.New("Could not decode key from " + keyFile)
		}
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return err
		}

		keys[key.PublicKey.N.String()] = key
	}

	b.Lock()
	b.keys = keys
	b.Unlock()

	return nil
}
