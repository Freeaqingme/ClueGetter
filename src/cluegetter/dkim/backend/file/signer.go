// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package file

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"hash"

	dkim "github.com/Freeaqingme/go-dkim"
)

type signer struct {
	backend *backend
	pubKey  *dkim.PubKey
}

func (b *backend) NewSigner(key *dkim.PubKey) (dkim.Signer, error) {
	signer := &signer{
		backend: b,
		pubKey:  key,
	}

	return signer, nil
}

func (s *signer) Sign(message []byte, algo string) (string, error) {
	key := s.backend.getPrivKey(&s.pubKey.PubKey)
	if key == nil {
		return "", errors.New("No private key found for pub key")
	}

	h1, h2, err := s.getHashandHash(algo)
	if err != nil {
		return "", err
	}

	h1.Write(message)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, *h2, h1.Sum(nil))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func (s *signer) getHashandHash(algo string) (hash.Hash, *crypto.Hash, error) {
	var h1 hash.Hash
	var h2 crypto.Hash
	switch algo {
	case "sha1":
		h1 = sha1.New()
		h2 = crypto.SHA1
		break
	case "sha256":
		h1 = sha256.New()
		h2 = crypto.SHA256
		break
	default:
		return nil, nil, errors.New("Invalid algo provided: " + algo)
	}

	return h1, &h2, nil
}
