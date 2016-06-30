// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package moduleDemo

import (
	"cluegetter/address"
	"cluegetter/core"
)

const ModuleName = "demo"

type module struct {
	*core.BaseModule

	cg *core.Cluegetter
}

func init() {
	core.ModuleRegister(&module{})
}

func (m *module) Name() string {
	return ModuleName
}

func (m *module) Enable() bool {
	return true
}

func (m *module) SetCluegetter(cg *core.Cluegetter) {
	m.cg = cg
}

func (m *module) Init() {
	m.cg.Log.Noticef("Initializing module demo")
}

func (m *module) Stop() {
	m.cg.Log.Noticef("Stopping module demo")
}

func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	m.cg.Log.Noticef("Milter Checking Message %s", msg.QueueId)

	return nil
}

func (m *module) RecipientCheck(rcpt *address.Address) (verdict int, msg string) {
	m.cg.Log.Debugf("Considering if we should accept recipient %s", rcpt.String())

	return core.MessagePermit, ""
}

func (m *module) Ipc() map[string]func(string) {
	return make(map[string]func(string), 0)
}

func (m *module) Rpc() map[string]chan string {
	return make(map[string]chan string, 0)
}

func (m *module) HttpHandlers() map[string]core.HttpCallback {
	return make(map[string]core.HttpCallback, 0)
}
