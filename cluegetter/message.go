// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

type Session interface {
	getSaslUsername() string
	getSaslSender() string
	getSaslMethod() string
	getCertIssuer() string
	getCertSubject() string
	getCipherBits() string
	getCipher() string
	getTlsVersion() string
	getIp() string
	getHostname() string
	getHelo() string
}

type Message interface {
	getQueueId() string
	getFrom() string
	getRcptCount() string
	getBody() string
	getHeaders() []*MessageHeader
}

type MessageHeader interface {
	getKey() string
	getValue() string
}

