package core

import (
	"github.com/Freeaqingme/GoDaemonSkeleton/log"
	"os"
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

func initCg() {
	cg = &Cluegetter{
		Config: Config,
		Log:    Log,
	}

	for _, module := range modules {
		module.SetCluegetter(cg)
	}
}
