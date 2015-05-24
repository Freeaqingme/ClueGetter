// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package http

import (
	"fmt"
	"log"
	"net"
	"net/http"
)

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Path: %s", r.URL.Path[1:])
}

func Start(c chan int) {
	laddr, err := net.ResolveTCPAddr("tcp", "0.0.0.0:8080")
	if nil != err {
		log.Fatalln(err)
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if nil != err {
		log.Fatalln(err)
	}
	log.Println("listening on", listener.Addr())

	http.HandleFunc("/", handler)
	go http.Serve(listener, nil)

	<-c
	listener.Close()
	log.Println("HTTP Listener closed")
	c <- 1
}
