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
	"reflect"
	"strconv"
	"syscall"
	"unsafe"
)

var Config = *new(config)
var Log = logging.MustGetLogger("cluegetter")

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
	moduleMgrStart()

	milterStart()
	PolicyStart()

	s := <-ch
	Log.Notice(fmt.Sprintf("Received '%s', exiting...", s.String()))

	PolicyStop()
	moduleMgrStop()
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
