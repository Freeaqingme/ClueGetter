// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
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
	return true
}

func (m *module) Init() {
	m.Log().Noticef("Initializing module demo")
}

func (m *module) Stop() {
	m.Log().Noticef("Stopping module demo")
}

func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	m.Log().Noticef("Milter Checking Message %s", msg.QueueId)

	return nil
}

func (m *module) RecipientCheck(rcpt *address.Address) (verdict int, msg string) {
	m.Log().Debugf("Considering if we should accept recipient %s", rcpt.String())

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
