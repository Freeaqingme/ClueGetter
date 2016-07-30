// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"flag"
	"fmt"
	"os"

	"cluegetter/core"

	skel "github.com/Freeaqingme/GoDaemonSkeleton"
	"github.com/Freeaqingme/GoDaemonSkeleton/log"
)

import (
	_ "cluegetter/bayes"
	_ "cluegetter/bounceHandler"
	_ "cluegetter/clamav"
	_ "cluegetter/dkim"
	_ "cluegetter/elasticsearch"
	_ "cluegetter/greylisting"
	_ "cluegetter/lua"
	_ "cluegetter/rspamd"
	_ "cluegetter/spamassassin"
	_ "cluegetter/srs"
	//_ "cluegetter/demo"
)

var (
	defaultConfigFile = "/etc/cluegetter/cluegetter.conf"
)

// Set by linker flags
var (
	buildTag  string
	buildTime string
)

func main() {
	app, args := skel.GetApp()

	if app.Name == "version" {
		// We don't want to require config stuff for merely displaying the version
		(*app.Handover)()
		return
	}

	configFile := flag.String("config", defaultConfigFile, "Path to Config File")
	logLevel := flag.String("loglevel", "DEBUG",
		"Log Level. One of: CRITICAL, ERROR, WARNING, NOTICE, INFO, DEBUG)")
	flag.Parse()

	core.Log = log.Open("ClueGetter", *logLevel)

	core.DefaultConfig(&core.Config)
	if *configFile != "" {
		core.LoadConfig(*configFile, &core.Config)
	}
	core.InitCg()

	os.Args = append([]string{os.Args[0]}, args...)
	(*app.Handover)()
}

func init() {
	handover := func() {
		fmt.Printf(
			"ClueGetter - Does things with mail - %s\n\n"+
				"%s\nCopyright (c) 2015-2016, Dolf Schimmel\n"+
				"License: Apache License, Version 2.0\n\n"+
				"Time of Build: %s\n\n",
			buildTag,
			"https://github.com/Freeaqingme/ClueGetter",
			buildTime)
		os.Exit(0)
	}

	skel.AppRegister(&skel.App{
		Name:     "version",
		Handover: &handover,
	})
}
