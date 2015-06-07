// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"net/http"
//	"fmt"
	"net"
	"html/template"
	"os"
)

func httpStart(done <-chan struct{}) {
	listen_host := Config.ClueGetter.Http_Listen_Host
	listen_port := Config.ClueGetter.Http_Listen_Port

	laddr, err := net.ResolveTCPAddr("tcp", listen_host+":"+listen_port)
	if nil != err {
		Log.Fatal(err)
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if nil != err {
		Log.Fatal(err)
	}
	Log.Info("HTTP interface now listening on %s", listener.Addr())

	http.HandleFunc("/", handler)
//	http.HandleFunc("/view/", handler)

	go http.Serve(listener, nil)

	go func() {
		<-done
		listener.Close()
		Log.Info("HTTP Listener closed")
	}()
}

type foo struct {
	Foo string
}

func handler(w http.ResponseWriter, r *http.Request) {
	foo := foo{Foo: "Blaat"}

	cwd, _ := os.Getwd()
	tmpl, err := template.ParseFiles(cwd + "/httpTemplates/skeleton.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, foo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

}
