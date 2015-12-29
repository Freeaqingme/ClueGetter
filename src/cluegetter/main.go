// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"flag"
	"fmt"
	logging "github.com/op/go-logging"
	"log"
	"log/syslog"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var (
	modulesMu sync.Mutex
	modules   = make([]*module, 0)
	Config    = *new(config)
	Log       = logging.MustGetLogger("cluegetter")
	instance  uint
)

type module struct {
	name        string
	init        *func()
	stop        *func()
	milterCheck *func(*Message, chan bool) *MessageCheckResult
}

func main() {

	configFile := flag.String("config", "", "Path to Config File")
	logFile := flag.String("logfile", "", "Log file to use. Will use STDOUT/STDERR otherwise.")
	logLevel := flag.String("loglevel", "NOTICE",
		"Log Level. One of: CRITICAL, ERROR, WARNING, NOTICE, INFO, DEBUG)")
	flag.Parse()

	initLogging(*logLevel, *logFile)
	Log.Notice("Starting ClueGetter...")

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	DefaultConfig(&Config)
	if *configFile != "" {
		LoadConfig(*configFile, &Config)
	}

	done := make(chan struct{})
	rdbmsStart()
	persistStart()
	cqlStart()
	setInstance()

	milterSessionStart()
	httpStart(done)
	messageStart()
	for _, module := range modules {
		if module.init != nil {
			(*module.init)()
		}
	}
	milterStart()

	s := <-ch
	Log.Notice(fmt.Sprintf("Received '%s', exiting...", s.String()))

	close(done)
	milterStop()
	for _, module := range modules {
		if module.stop != nil {
			(*module.stop)()
		}
	}
	messageStop()
	cqlStop()
	rdbmsStop()

	Log.Notice("Successfully ceased all operations.")
	os.Exit(0)
}

func initLogging(logLevelStr string, logPath string) {
	logLevel, err := logging.LogLevel(logLevelStr)
	if err != nil {
		log.Fatal("Invalid log level specified")
	}

	var formatStdout = logging.MustStringFormatter(
		"%{color}%{time:2006-01-02T15:04:05.000} %{shortfunc} ▶ %{level:.4s} %{color:reset} %{message}",
	)
	stdout := logging.NewLogBackend(os.Stdout, "", 0)
	formatter := logging.NewBackendFormatter(stdout, formatStdout)
	stdoutLeveled := logging.AddModuleLevel(formatter)
	stdoutLeveled.SetLevel(logLevel, "")
	syslogBackend, err := logging.NewSyslogBackendPriority("cluegetter", syslog.LOG_MAIL)
	if err != nil {
		Log.Fatal(err)
	}

	if logPath != "" {
		logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			Log.Fatal(err)
		}
		syscall.Dup2(int(logFile.Fd()), 1)
		syscall.Dup2(int(logFile.Fd()), 2)
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

func ModuleRegister(module *module) {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	if module == nil {
		panic("Module: Register module is nil")
	}
	for _, dup := range modules {
		if dup.name == module.name {
			panic("Module: Register called twice for module " + module.name)
		}
	}
	modules = append(modules, module)
}
