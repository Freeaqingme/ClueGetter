// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/Freeaqingme/GoDaemonSkeleton"
	"github.com/Freeaqingme/GoDaemonSkeleton/log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

var (
	ipcHandlers    = make(map[string]func(string), 0)
	instance       uint
	defaultLogFile = "/var/log/cluegetter.log"
	logFile        string
)

func init() {
	handover := daemonStart

	GoDaemonSkeleton.AppRegister(&GoDaemonSkeleton.App{
		Name:     "daemon",
		Handover: &handover,
	})
}

func DaemonReset() {
	modules = make([]Module, 0)
	ipcHandlers = make(map[string]func(string), 0)
}

func daemonStart() {
	initCg()

	logFileTmp := flag.String("logfile", defaultLogFile, "Log file to use.")
	foreground := flag.Bool("foreground", false, "Run in Foreground")
	flag.Parse()

	if !*foreground {
		logFile = *logFileTmp
		log.LogRedirectStdOutToFile(logFile)
	}
	Log.Notice("Starting ClueGetter...")

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})
	rdbmsStart()
	setInstance()
	redisStart()

	milterSessionStart()
	httpStart(done)
	messageStart()
	for _, module := range modules {
		if !module.Enable() {
			Log.Info("Skipping module '" + module.Name() + "' because it was not enabled")
			continue
		}
		module.Init()
		Log.Info("Module '" + module.Name() + "' started successfully")
	}
	milterStart()

	go daemonIpc(done)
	s := <-ch
	Log.Notice(fmt.Sprintf("Received '%s', exiting...", s.String()))

	close(done)
	milterStop()
	for _, module := range modules {
		module.Stop()
	}
	messageStop()
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

func daemonIpc(done <-chan struct{}) {
	if _, err := os.Stat(Config.ClueGetter.IPC_Socket); !os.IsNotExist(err) {
		err = os.Remove(Config.ClueGetter.IPC_Socket)
		if err != nil {
			Log.Fatal(fmt.Sprintf("IPC Socket %s already exists and could not be removed: %s",
				Config.ClueGetter.IPC_Socket, err.Error()))
		}
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
	defer CluegetterRecover("daemonIpcHandleConn")
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
			v = strings.TrimRightFunc(kv[1], func(v rune) bool { return v == '\x00' })
		}
		if callback == nil {
			Log.Debug("Received IPC message but no such pattern was registered, ignoring: <%s>%s", handle, v)
			return
		}

		callback(v)
	}
}

func daemonIpcSend(handle string, message string) {
	c, err := net.Dial("unix", Config.ClueGetter.IPC_Socket)
	if err != nil {
		// TODO: Why does log.fatal not write to stderr?
		os.Stderr.WriteString("Could not connect to ICP socket: " + err.Error() + "\n")
		os.Exit(1)
	}
	defer c.Close()

	msg := []byte(handle + " " + message)
	msg = append(msg, '\x00')
	_, err = c.Write(msg)
	if err != nil {
		Log.Fatal("write error:", err)
	}
}
