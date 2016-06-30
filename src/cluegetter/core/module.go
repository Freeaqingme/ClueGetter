// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"cluegetter/address"
)

type Module interface {
	SetCluegetter(*Cluegetter)
	Name() string
	Enable() bool
	Init()
	Stop()
	MessageCheck(msg *Message, done chan bool) *MessageCheckResult
	RecipientCheck(rcpt *address.Address) (verdict int, msg string)
	SessionDisconnect(s *MilterSession)
	Ipc() map[string]func(string)
	Rpc() map[string]chan string
	HttpHandlers() map[string]HttpCallback
}

func ModuleRegister(module Module) {
	cg.modulesMu.Lock()
	defer cg.modulesMu.Unlock()
	if module == nil {
		panic("Module: Register module is nil")
	}

	if module.Name() == "" {
		panic("Module: No name was set")
	}

	for _, dup := range cg.modules {
		if dup.Name() == module.Name() {
			panic("Module: Register called twice for module " + module.Name())
		}
	}

	if ipc := module.Ipc(); ipc != nil {
		for ipcName, ipcCallback := range ipc {
			if _, ok := ipcHandlers[ipcName]; ok {
				panic("Tried to register ipcHandler twice for " + ipcName)
			}
			ipcHandlers[ipcName] = ipcCallback
		}
	}

	cg.modules = append(cg.modules, module)
}

func (cg *Cluegetter) Modules() []Module {
	out := make([]Module, 0)
	for _, module := range cg.modules {
		if module.Enable() {
			out = append(out, module)
		}
	}
	return out
}

func (cg *Cluegetter) Module(name, caller string) *Module {
	cg.modulesMu.RLock()
	defer cg.modulesMu.RUnlock()

	for _, module := range cg.modules {
		if module.Name() != name {
			continue
		}

		if module.Enable() {
			return &module
		} else {
			break
		}
	}

	if caller != "" {
		panic("Module " + caller + " requires module " + name + " but it was not found (or enabled)")
	}

	return nil
}

//
// BaseModule
//
// You'll need to implement yourself at least the following methods:
//
// Name() string
// Enable() bool
// SetCluegetter(cg *Cluegetter)
//
type BaseModule struct{}

func (m *BaseModule) Init() {}

func (m *BaseModule) Stop() {}

func (m *BaseModule) MessageCheck(msg *Message, done chan bool) *MessageCheckResult {
	return nil
}

func (m *BaseModule) RecipientCheck(rcpt *address.Address) (verdict int, msg string) {
	return MessagePermit, ""
}

func (m *BaseModule) SessionDisconnect(s *MilterSession) {}

func (m *BaseModule) Ipc() map[string]func(string) {
	return make(map[string]func(string), 0)
}

func (m *BaseModule) Rpc() map[string]chan string {
	return make(map[string]chan string, 0)
}

func (m *BaseModule) HttpHandlers() map[string]HttpCallback {
	return make(map[string]HttpCallback, 0)
}

//
// ModuleOld
//

type ModuleOld struct {
	*BaseModule

	name         string
	enable       *func() bool
	init         *func()
	stop         *func()
	milterCheck  *func(*Message, chan bool) *MessageCheckResult
	ipc          map[string]func(string)
	rpc          map[string]chan string
	httpHandlers map[string]HttpCallback
}

func (m *ModuleOld) Name() string {
	return m.name
}

func (m *ModuleOld) SetCluegetter(cg *Cluegetter) {
}

func (m *ModuleOld) Enable() bool {
	if m.enable == nil {
		return false
	}

	return (*m.enable)()
}

func (m *ModuleOld) Init() {
	if m.init == nil {
		return
	}

	(*m.init)()
}

func (m *ModuleOld) Stop() {
	if m.stop == nil {
		return
	}

	(*m.stop)()
}

func (m *ModuleOld) MessageCheck(msg *Message, done chan bool) *MessageCheckResult {
	if m.milterCheck == nil {
		return nil
	}

	return (*m.milterCheck)(msg, done)
}

func (m *ModuleOld) RecipientCheck(rcpt *address.Address) (verdict int, msg string) {
	return MessagePermit, ""
}

func (m *ModuleOld) Ipc() map[string]func(string) {
	if m.ipc == nil {
		return make(map[string]func(string), 0)
	}

	return m.ipc
}

func (m *ModuleOld) Rpc() map[string]chan string {
	if m.rpc == nil {
		return make(map[string]chan string, 0)
	}
	return m.rpc
}

func (m *ModuleOld) HttpHandlers() map[string]HttpCallback {
	if m.httpHandlers == nil {
		return make(map[string]HttpCallback, 0)
	}
	return m.httpHandlers
}
