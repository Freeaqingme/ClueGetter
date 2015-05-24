// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"cluegetter/http"
//	"cluegetter/postfix"
	"fmt"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

var Config = *new(config)

func main() {

	configFile := flag.String("config", "", "Path to Config File")
	flag.Parse()

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	httpControl := make(chan int)
	postfixPolicyControl := make(chan int)

	keepRunning := false
	for {
		DefaultConfig(&Config)
		if *configFile != "" {
			LoadConfig(*configFile, &Config)
		}

		go http.Start(httpControl)
		go PolicyStart(
				postfixPolicyControl,
				Config.ClueGetter.Stats_Listen_Host,
				Config.ClueGetter.Stats_Listen_Port)

		s := <-ch
		if s.String() == "hangup" {
			log.Println(fmt.Sprintf("Received '%s', reloading...", s.String()))
			keepRunning = true
		} else {
			log.Println(fmt.Sprintf("Received '%s', exiting...", s.String()))
			keepRunning = false
		}

		httpControl <- 1
		postfixPolicyControl <- 1

		<-httpControl
		<-postfixPolicyControl

		if !keepRunning {
			break
		}
	}

	log.Println("Successfully ceased all operations.")
	os.Exit(0)
}
