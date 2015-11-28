// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"sync"
)

type module struct {
	name        string
	init        *func()
	stop        *func()
	milterCheck *func(*Message, chan bool) *MessageCheckResult
}

var (
	modulesMu sync.Mutex
	modules   = make([]*module, 0)
)

// Register makes a database driver available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func Register(module *module) {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	if module == nil {
		panic("Module: Register module is nil")
	}
	for _, dup := range modules {
		if dup.name == module.name {
			panic("Module: Register called twice for module " + module.name)
		}
	}
	modules = append(modules, module)
}
