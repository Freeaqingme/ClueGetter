// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
)

type subApp struct {
	name     string
	handover *func()
}

var (
	subAppsMu         sync.Mutex
	subApps           = make([]*subApp, 0)
	defaultConfigFile = "/etc/cluegetter/cluegetter.conf"
	Config            = *new(config)
	hostname, _       = os.Hostname()
)

func main() {
	subAppNames := func() []string {
		out := []string{}
		for _, subApp := range subApps {
			out = append(out, subApp.name)
		}

		return out
	}

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "No Sub-App specified. Must be one of: %s\n", strings.Join(subAppNames(), " "))
		os.Exit(1)
	}

	var subAppArgs []string
	var subApp *subApp
OuterLoop:
	for i, arg := range os.Args[1:] {
		for _, subAppTemp := range subApps {
			if arg == subAppTemp.name {
				subApp = subAppTemp
				subAppArgs = os.Args[i+2:]
				os.Args = os.Args[:i+1]
				break OuterLoop
			}
		}
	}

	if subApp == nil {
		fmt.Fprintf(os.Stderr, "No Sub-App specified. Must be one of: %s\n", strings.Join(subAppNames(), " "))
		os.Exit(1)
	}

	configFile := flag.String("config", defaultConfigFile, "Path to Config File")
	logLevel := flag.String("loglevel", "NOTICE",
		"Log Level. One of: CRITICAL, ERROR, WARNING, NOTICE, INFO, DEBUG)")
	flag.Parse()

	logSetupGlobal(*logLevel)

	DefaultConfig(&Config)
	if *configFile != "" {
		LoadConfig(*configFile, &Config)
	}

	os.Args = append([]string{os.Args[0]}, subAppArgs...)
	(*subApp.handover)()
}

func subAppRegister(subApp *subApp) {
	subAppsMu.Lock()
	defer subAppsMu.Unlock()
	if subApp == nil {
		panic("nil subapp supplied")
	}
	for _, dup := range subApps {
		if dup.name == subApp.name {
			panic("Register called twice for subApp " + subApp.name)
		}
	}
	subApps = append(subApps, subApp)
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
