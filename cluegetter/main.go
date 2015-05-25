// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"cluegetter/cluegetter/http"
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
	setProcessName("cluegetter")

	configFile := flag.String("config", "", "Path to Config File")
	logLevel := flag.String("loglevel", "NOTICE",
		"Log Level. One of: CRITICAL, ERROR, WARNING, NOTICE, INFO, DEBUG)")
	flag.Parse()

	initLogging(*logLevel)
	Log.Notice("Starting ClueGetter...")

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	httpControl := make(chan int)
	rdbmsControl := make(chan int)
	moduleControl := make(chan int)
	quotasControl := make(chan int)
	postfixPolicyControl := make(chan int)

	keepRunning := false
	for {
		DefaultConfig(&Config)
		if *configFile != "" {
			LoadConfig(*configFile, &Config)
		}

		go rdbmsStart(rdbmsControl)
		<-rdbmsControl // Wait until connected with RDBMS

		go http.Start(httpControl)
		go quotasStart(quotasControl)
		go moduleStart(moduleControl)
		<-quotasControl
		<-moduleControl
		go PolicyStart(
			postfixPolicyControl,
			Config.ClueGetter.Stats_Listen_Host,
			Config.ClueGetter.Stats_Listen_Port)

		s := <-ch
		if s.String() == "hangup" {
			Log.Notice(fmt.Sprintf("Received '%s', reloading...", s.String()))
			keepRunning = true
		} else {
			Log.Notice(fmt.Sprintf("Received '%s', exiting...", s.String()))
			keepRunning = false
		}

		httpControl <- 1
		postfixPolicyControl <- 1
		quotasControl <- 1
		moduleControl <- 1
		rdbmsControl <- 1

		<-httpControl
		<-postfixPolicyControl
		<-quotasControl
		<-moduleControl
		<-rdbmsControl

		if !keepRunning {
			break
		}
	}

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

func setProcessName(name string) error {
	argv0str := (*reflect.StringHeader)(unsafe.Pointer(&os.Args[0]))
	argv0 := (*[1 << 30]byte)(unsafe.Pointer(argv0str.Data))[:argv0str.Len]

	paddedName := fmt.Sprintf("%-"+strconv.Itoa(len(argv0))+"s", name)
	if len(paddedName) > len(argv0) {
		panic("Cannot set proccess name that is longer than original argv[0]")
	}

	n := copy(argv0, paddedName)
	if n < len(argv0) {
		argv0[n] = 0
	}

	return nil
}
