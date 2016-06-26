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
			cg.Log.Fatal("No instance was set")
		}

		err := cg.Rdbms().QueryRow("SELECT id from instance WHERE name = ?", cg.Config.ClueGetter.Instance).
			Scan(&instance)
		if err != nil {
			cg.Log.Fatal(fmt.Sprintf("Could not retrieve instance '%s' from database: %s",
				cg.Config.ClueGetter.Instance, err))
		}

		Log.Notice("Instance name: %s. Id: %d", cg.Config.ClueGetter.Instance, instance)
		cg.instance = instance
	}

	return cg.instance
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
	Log.Error("Panic caught in %s(). Recovering. Error: %s", funcName, r)
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
