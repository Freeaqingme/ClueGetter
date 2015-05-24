package main

import (
	"cluegetter/http"
	"cluegetter/postfix"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	httpControl := make(chan int)
	postfixPolicyControl := make(chan int)

	keepRunning := false
	for {
		go http.Start(httpControl)
		go postfix.PolicyStart(postfixPolicyControl)

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
