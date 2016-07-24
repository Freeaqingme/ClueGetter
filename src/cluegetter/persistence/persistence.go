// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package persistence

import (
	"cluegetter/core"
	"cluegetter/persistence/cockroachdb"
)

const ModuleName = "persistence"

type module struct {
	*core.BaseModule

	cg *core.Cluegetter

	registry []db
}

type db interface {
	Type() string
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
	return true
}

func (m *module) Init() {
	m.registry = make([]db, 0)
	for _, db := range cockroachdb.Init(m.cg) {
		m.registry = append(m.registry, db)
	}
}
