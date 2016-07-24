// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package base

import "cluegetter/core"

type Db struct {
	cg     *core.Cluegetter
	name   string
	dbType string
}

type module interface {
	Name() string
}

func (db *Db) Type() string {
	return db.dbType
}

func NewDb(cg *core.Cluegetter, name string, dbType string) *Db {
	return &Db{
		cg:     cg,
		name:   name,
		dbType: dbType,
	}
}
