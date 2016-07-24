// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package spamassassin

import (
	"strconv"

	"cluegetter/core"

	"github.com/Freeaqingme/go-spamc"
)

func (m *module) BayesLearn(msg *core.Message, isSpam bool) {
	bodyStr := string(msg.String())

	host := m.Config().SpamAssassin.Host + ":" + strconv.Itoa(m.Config().SpamAssassin.Port)
	sconf := msg.Session().Config().SpamAssassin
	client := spamc.New(host, sconf.Timeout, sconf.Connect_Timeout)

	abort := make(chan bool) // unused really. To implement or not to implement. That's the question
	if isSpam {
		client.Learn(abort, spamc.LEARN_SPAM, bodyStr)
	} else {
		client.Learn(abort, spamc.LEARN_HAM, bodyStr)
	}
	close(abort)
}
