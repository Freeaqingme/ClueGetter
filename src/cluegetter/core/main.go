package core

import (
	"database/sql"
	"fmt"
	"os"
	"sync"

	"github.com/Freeaqingme/GoDaemonSkeleton/log"
)

var (
	Config      = *new(config)
	hostname, _ = os.Hostname()
	Log         *log.Logger
	cg          = &Cluegetter{modules: make([]Module, 0)}
)

type Cluegetter struct {
	config *config
	log    *log.Logger
	redis  RedisClient

	instance   uint
	instanceMu sync.Mutex

	modulesMu sync.RWMutex
	modules   []Module
}

func (cg *Cluegetter) Config() config {
	return *cg.config
}

func (cg *Cluegetter) Log() *log.Logger {
	return cg.log
}

func (cg *Cluegetter) Redis() RedisClient {
	return cg.redis
}

func (cg *Cluegetter) Instance() uint {
	cg.instanceMu.Lock()
	defer cg.instanceMu.Unlock()
	if cg.instance == 0 {
		if cg.config.ClueGetter.Instance == "" {
			cg.log.Fatalf("No instance was set")
		}

		err := cg.Rdbms().QueryRow("SELECT id from instance WHERE name = ?", cg.config.ClueGetter.Instance).
			Scan(&instance)
		if err != nil {
			cg.log.Fatalf(fmt.Sprintf("Could not retrieve instance '%s' from database: %s",
				cg.config.ClueGetter.Instance, err))
		}

		Log.Noticef("Instance name: %s. Id: %d", cg.config.ClueGetter.Instance, instance)
		cg.instance = instance
	}

	return cg.instance
}

func (cg *Cluegetter) Hostname() string {
	return hostname
}

// TODO: This will become the persistence layer
func (cg *Cluegetter) Rdbms() *sql.DB {
	if Rdbms == nil {
		rdbmsStart()
	}

	return Rdbms
}

func (cg *Cluegetter) NewMilterSession() *MilterSession {
	return &MilterSession{
		config: (*cg.config).sessionConfig(),
	}
}

func NewCluegetter() *Cluegetter {
	return &Cluegetter{
		modules: make([]Module, 0),
		config:  GetNewConfig(),
	}
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
	cg.config = &Config
	cg.log = Log
	cg.instance = instance

	for _, module := range cg.modules {
		module.SetCluegetter(cg)
	}

	return cg
}
