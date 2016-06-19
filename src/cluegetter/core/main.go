package core

import (
	"github.com/Freeaqingme/GoDaemonSkeleton/log"
	"os"
)

var (
	Config      = *new(config)
	hostname, _ = os.Hostname()
	Log         *log.Logger
)

type Cluegetter struct {
	Config config
	Log    *log.Logger
}

func cluegetterRecover(funcName string) {
	if Config.ClueGetter.Exit_On_Panic {
		return
	}
	r := recover()
	if r == nil {
		return
	}
	Log.Error("Panic caught in %s(). Recovering. Error: %s", funcName, r)
}
