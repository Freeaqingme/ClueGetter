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
	cg.modules = make([]Module, 0) // Todo: This is probably abundant
	ipcHandlers = make(map[string]func(string), 0)
}

func daemonStart() {
	logFileTmp := flag.String("logfile", defaultLogFile, "Log file to use.")
	foreground := flag.Bool("foreground", false, "Run in Foreground")
	flag.Parse()

	if !*foreground {
		logFile = *logFileTmp
		log.LogRedirectStdOutToFile(logFile)
	}
	Log.Noticef("Starting ClueGetter...")

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})
	rdbmsStart()
	instance = cg.Instance()
	redisStart()

	milterSessionStart()
	httpStart(done)
	messageStart()
	for _, module := range cg.Modules() {
		module.Init()
		Log.Infof("Module '" + module.Name() + "' started successfully")
	}
	milterStart()

	go daemonIpc(done)
	s := <-ch
	Log.Noticef(fmt.Sprintf("Received '%s', exiting...", s.String()))

	close(done)
	milterStop()
	for _, module := range cg.Modules() {
		module.Stop()
	}
	messageStop()
	rdbmsStop()

	Log.Noticef("Successfully ceased all operations.")
	os.Exit(0)
}

func daemonIpc(done <-chan struct{}) {
	if _, err := os.Stat(Config.ClueGetter.IPC_Socket); !os.IsNotExist(err) {
		err = os.Remove(Config.ClueGetter.IPC_Socket)
		if err != nil {
			Log.Fatalf(fmt.Sprintf("IPC Socket %s already exists and could not be removed: %s",
				Config.ClueGetter.IPC_Socket, err.Error()))
		}
	}

	l, err := net.ListenUnix("unix", &net.UnixAddr{Config.ClueGetter.IPC_Socket, "unix"})
	if err != nil {
		Log.Fatalf("%s", err)
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
				Log.Infof("IPC Socket shutting down")
				break
			}
			Log.Fatalf("Critical error on IPC Socket: %s", err)
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
				Log.Infof("Got %s on IPC Socket. Ignoring.", err.Error())
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
			Log.Debugf("Received IPC message but no such pattern was registered, ignoring: <%s>%s", handle, v)
			return
		}

		callback(v)
	}
}

func DaemonIpcSend(handle string, message string) {
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
		Log.Fatalf("write error:", err)
	}
}
