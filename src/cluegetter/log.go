// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	logging "github.com/op/go-logging"
	"log"
	"log/syslog"
	"os"
	"syscall"
)

var (
	Log = logging.MustGetLogger("cluegetter")
)

func init() {
	handover := logHandover

	subAppRegister(&subApp{
		name:     "log",
		handover: &handover,
	})

	ModuleRegister(&module{
		name: "log",
		ipc: map[string]func(string){
			"log!reopen": logReopen,
		},
	})
}

func logHandover() {
	if len(os.Args) < 2 || os.Args[1] != "reopen" {
		Log.Fatal("Missing argument for 'log'. Must be one of: reopen")
	}

	daemonIpcSend("log!reopen", "")
}

func logReopen(args string) {
	if logFile == "" {
		Log.Notice("Asked to reopen logs but running in foreground. Ignoring.")
		return
	}
	Log.Notice("Reopening log file per IPC request...")
	logRedirectStdOutToFile(logFile)
	Log.Notice("Reopened log file per IPC request")
}

func logSetupGlobal(logLevelStr string) {
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

	logging.SetBackend(syslogBackend, stdoutLeveled)
}

func logRedirectStdOutToFile(logPath string) {
	if logPath == "" {
		Log.Fatal("Log Path not set")
	}

	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		Log.Fatal(err)
	}
	syscall.Dup2(int(logFile.Fd()), 1)
	syscall.Dup2(int(logFile.Fd()), 2)
}
