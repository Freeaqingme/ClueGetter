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
	cg          *Cluegetter
)

type Cluegetter struct {
	Config config
	Log    *log.Logger
	Redis  RedisClient

	instance uint
	indexMu  sync.Mutex
}

func (cg *Cluegetter) Instance() uint {
	cg.indexMu.Lock()
	defer cg.indexMu.Unlock()
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
	cg = &Cluegetter{
		Config:   Config,
		Log:      Log,
		instance: instance,
	}

	for _, module := range modules {
		module.SetCluegetter(cg)
	}

	return cg
}
