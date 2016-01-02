// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

var (
	modulesMu      sync.Mutex
	modules        = make([]*module, 0)
	ipcHandlers    = make(map[string]func(string), 0)
	instance       uint
	defaultLogFile = "/var/log/cluegetter.log"
	logFile        string
)

type module struct {
	name         string
	enable       *func() bool
	init         *func()
	stop         *func()
	milterCheck  *func(*Message, chan bool) *MessageCheckResult
	ipc          map[string]func(string)
	rpc          map[string]chan string
	httpHandlers map[string]httpCallback
}

func init() {
	handover := daemonStart

	subAppRegister(&subApp{
		name:     "daemon",
		handover: &handover,
	})
}

func daemonStart() {
	logFileTmp := flag.String("logfile", defaultLogFile, "Log file to use.")
	foreground := flag.Bool("foreground", false, "Run in Foreground")
	flag.Parse()

	if !*foreground {
		logFile = *logFileTmp
		logRedirectStdOutToFile(logFile)
	}
	Log.Notice("Starting ClueGetter...")

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})
	rdbmsStart()
	setInstance()
	persistStart()
	cqlStart()

	milterSessionStart()
	httpStart(done)
	messageStart()
	for _, module := range modules {
		if module.enable != nil && !(*module.enable)() {
			Log.Info("Skipping module '%s' because it was not enabled", module.name)
			continue
		}
		if module.init != nil {
			(*module.init)()
			Log.Info("Module '%s' started successfully", module.name)
		}
	}
	milterStart()

	go daemonIpc(done)
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

	if module.ipc != nil {
		for ipcName, ipcCallback := range module.ipc {
			if _, ok := ipcHandlers[ipcName]; ok {
				panic("Tried to register ipcHandler twice for " + ipcName)
			}
			ipcHandlers[ipcName] = ipcCallback
		}
	}

	modules = append(modules, module)
}

func daemonIpc(done <-chan struct{}) {
	if _, err := os.Stat(Config.ClueGetter.IPC_Socket); !os.IsNotExist(err) {
		// Is this ideal? Can we do this any better? Need moar resilliency
		Log.Fatal(fmt.Sprintf(
			"IPC Socket %s already exists. Can be the result of an unclean shut down "+
				"or because Cluegetter is already running. Manual intervention required.",
			Config.ClueGetter.IPC_Socket,
		))
	}

	l, err := net.ListenUnix("unix", &net.UnixAddr{Config.ClueGetter.IPC_Socket, "unix"})
	if err != nil {
		Log.Fatal(err)
	}

	go func() {
		<-done
		l.Close() // Also removes socket file
	}()

	for {
		conn, err := l.AcceptUnix()
		if err != nil {
			_, open := <-done
			if !open {
				Log.Info("IPC Socket shutting down")
				break
			}
			Log.Fatal("Critical error on IPC Socket: %s", err)
		}

		go daemonIpcHandleConn(conn)
	}
}

func daemonIpcHandleConn(conn *net.UnixConn) {
	defer cluegetterRecover("daemonIpcHandleConn")
	defer conn.Close()

	for {
		message, err := bufio.NewReader(conn).ReadString('\x00')
		if err != nil {
			if message != "" {
				Log.Info("Got %s on IPC Socket. Ignoring.", err.Error())
			}
			return
		}

		kv := strings.SplitN(message, " ", 2)
		handle := strings.TrimRightFunc(kv[0], func(v rune) bool { return v == '\x00' })
		callback := ipcHandlers[handle]
		v := ""
		if len(kv) > 1 {
			v = kv[1]
		}
		if callback == nil {
			Log.Debug("Received IPC message but no such pattern was registered, ignoring: <%s>%s", handle, v)
			return
		}

		callback(v)
		// conn.Write([]byte(message))
	}
}

func daemonIpcSend(handle string, message string) {
	c, err := net.Dial("unix", Config.ClueGetter.IPC_Socket)
	if err != nil {
		panic(err)
	}
	defer c.Close()

	msg := []byte(handle + " " + message)
	msg = append(msg, '\x00')
	_, err = c.Write(msg)
	if err != nil {
		Log.Fatal("write error:", err)
	}
}
