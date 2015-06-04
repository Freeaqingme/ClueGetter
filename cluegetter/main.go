// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"flag"
	"fmt"
	"github.com/op/go-logging"
	"log"
	"os"
	"os/signal"
	"syscall"
)

var Config = *new(config)
var Log = logging.MustGetLogger("cluegetter")
var instance uint

func Main() {

	configFile := flag.String("config", "", "Path to Config File")
	logLevel := flag.String("loglevel", "NOTICE",
		"Log Level. One of: CRITICAL, ERROR, WARNING, NOTICE, INFO, DEBUG)")
	flag.Parse()

	initLogging(*logLevel)
	Log.Notice("Starting ClueGetter...")

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	DefaultConfig(&Config)
	if *configFile != "" {
		LoadConfig(*configFile, &Config)
	}

	statsStart()
	rdbmsStart()
	setInstance()

	messageStart()
	quotasStart()
	milterStart()

	s := <-ch
	Log.Notice(fmt.Sprintf("Received '%s', exiting...", s.String()))

	milterStop()
	quotasStop()
	messageStop()
	rdbmsStop()

	Log.Notice("Successfully ceased all operations.")
	os.Exit(0)
}

func initLogging(logLevelStr string) {
	logLevel, err := logging.LogLevel(logLevelStr)
	if err != nil {
		log.Fatal("Invalid log level specified")
	}

	var formatStdout = logging.MustStringFormatter(
		"%{color}%{time:15:04:05.000} %{shortfunc} ▶ %{level:.4s} %{color:reset} %{message}",
	)
	stdout := logging.NewLogBackend(os.Stdout, "", 0)
	formatter := logging.NewBackendFormatter(stdout, formatStdout)
	stdoutLeveled := logging.AddModuleLevel(formatter)
	stdoutLeveled.SetLevel(logLevel, "")
	syslogBackend, err := logging.NewSyslogBackend("")
	if err != nil {
		Log.Fatal(err)
	}

	logging.SetBackend(syslogBackend, stdoutLeveled)
}

func setInstance() {
	if Config.ClueGetter.Instance == "" {
		Log.Fatal("No instance was set")
	}

	err := Rdbms.QueryRow("SELECT id from instance WHERE name = ?", Config.ClueGetter.Instance).Scan(&instance)
	if err != nil {
		Log.Fatal(fmt.Sprintf("Could not retrieve instance '%s' from database: %s",
			Config.ClueGetter.Instance, err))
	}

	Log.Notice("Instance name: %s. Id: %d", Config.ClueGetter.Instance, instance)
}
