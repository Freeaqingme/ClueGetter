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

	http.HandleFunc("/stats/", httpStatsHandler)
	http.HandleFunc("/", httpIndexHandler)

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

func httpIndexHandler(w http.ResponseWriter, r *http.Request) {
	foo := foo{Foo: "Blaat"}

	cwd, _ := os.Getwd()
	tpl := template.Must(template.ParseFiles(cwd + "/htmlTemplates/index.html", cwd + "/htmlTemplates/skeleton.html"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "skeleton.html", foo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

}


func httpStatsHandler(w http.ResponseWriter, r *http.Request) {
	foo := foo{Foo: "Blaat"}

	//	templates := make(map[string]*template.Template)

	cwd, _ := os.Getwd()
	tpl := template.Must(template.ParseFiles(cwd + "/htmlTemplates/stats.html", cwd + "/htmlTemplates/skeleton.html"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "skeleton.html", foo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

}
