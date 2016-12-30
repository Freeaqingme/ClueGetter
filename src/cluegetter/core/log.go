// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package core

import(
	"os"

	"github.com/Freeaqingme/GoDaemonSkeleton"
	"github.com/Freeaqingme/GoDaemonSkeleton/log"
)

func init() {
	handover := logHandover

	GoDaemonSkeleton.AppRegister(&GoDaemonSkeleton.App{
		Name:     "log",
		Handover: &handover,
	})

	ModuleRegister(&ModuleOld{
		name: "log",
		ipc: map[string]func(string){
			"log!reopen": logReopen,
		},
	})

}

func logHandover() {
	    if len(os.Args) != 2 || os.Args[1] != "reopen" {
		Log.Fatal("Missing argument for 'log'. Must be one of: reopen")
	}

	DaemonIpcSend("log!reopen", "")
}

func logReopen(args string) {
	log.Reopen()
}
