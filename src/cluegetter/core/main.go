// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"database/sql"
	"fmt"
	"github.com/Freeaqingme/GoDaemonSkeleton/log"
	"os"
	"sync"
)

var (
	Config      = *new(config)
	hostname, _ = os.Hostname()
	Log         *log.Logger
	cg          = &Cluegetter{modules: make([]Module, 0)}
)

type Cluegetter struct {
	Config config
	Log    *log.Logger
	Redis  RedisClient

	instance   uint
	instanceMu sync.Mutex

	modulesMu sync.RWMutex
	modules   []Module
}

func (cg *Cluegetter) Instance() uint {
	cg.instanceMu.Lock()
	defer cg.instanceMu.Unlock()
	if cg.instance == 0 {
		if cg.Config.ClueGetter.Instance == "" {
			cg.Log.Fatalf("No instance was set")
		}

		err := cg.Rdbms().QueryRow("SELECT id from instance WHERE name = ?", cg.Config.ClueGetter.Instance).
			Scan(&instance)
		if err != nil {
			cg.Log.Fatalf(fmt.Sprintf("Could not retrieve instance '%s' from database: %s",
				cg.Config.ClueGetter.Instance, err))
		}

		Log.Noticef("Instance name: %s. Id: %d", cg.Config.ClueGetter.Instance, instance)
		cg.instance = instance
	}

	return cg.instance
}

func (cg *Cluegetter) Hostname() string {
	return hostname
}

func (cg *Cluegetter) Rdbms() *sql.DB {
	if Rdbms == nil {
		rdbmsStart()
	}

	return Rdbms
}

func CluegetterRecover(funcName string) {
	if Config.ClueGetter.Exit_On_Panic {
		return
	}
	r := recover()
	if r == nil {
		return
	}
	Log.Errorf("Panic caught in %s(). Recovering. Error: %s", funcName, r)
}

func InitCg() *Cluegetter {
	cg.Config = Config
	cg.Log = Log
	cg.instance = instance

	for _, module := range cg.modules {
		module.SetCluegetter(cg)
	}

	return cg
}
