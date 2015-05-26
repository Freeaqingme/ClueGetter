// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//

/**
* See also: http://www.postfix.org/SMTPD_POLICY_README.html
**/

package cluegetter

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

func PolicyStart(c chan int, listen_host string, listen_port string) {
	l, err := net.Listen("tcp", listen_host+":"+listen_port)
	if nil != err {
		Log.Fatal(err)
	}

	Log.Notice("Now listening on " + listen_host + ":" + listen_port)
	go policyWaitForConnections(l)

	<-c
	l.Close()
	c <- 1
}

func policyWaitForConnections(l net.Listener) {
	backoffTime := float32(0)

	for {
		conn, err := l.Accept()
		if err != nil && backoffTime < 8000 {
			backoffTime = (backoffTime * 1.02) + 250
			Log.Error("Could not accept connection: %s. Backing off for %d",
				err, backoffTime)
			time.Sleep(time.Duration(backoffTime) * time.Millisecond)
			break
		} else if err != nil {
			Log.Fatal("Could not accept new connection. Backing out: %d", err)
		}

		backoffTime = backoffTime / 1.1
		go policyHandleConnection(conn)
	}
}

func policyHandleConnection(conn net.Conn) {
	defer conn.Close()

	Log.Debug("Received new coonnection from %s to %s", conn.RemoteAddr().String(), conn.LocalAddr().String())
	scanner := bufio.NewScanner(conn)
	for {
		message := ""
		for {
			if ok := scanner.Scan(); !ok {
				Log.Debug("Lost connection from %s to %s", conn.RemoteAddr().String(), conn.LocalAddr().String())
				return // Connection closed
			}
			s := scanner.Text()
			if s == "" {
				break
			}
			message = message + "\n" + s
		}

		if err := scanner.Err(); err != nil {
			Log.Warning("Error with connection from %s to %s: %s",
				conn.RemoteAddr().String(), conn.LocalAddr().String())
			return
		}

		response := policyGetResponseForMessage(strings.TrimSpace(message), conn.RemoteAddr().String())
		Log.Debug("Sent message from %s to %s: %s", conn.LocalAddr().String(), conn.RemoteAddr().String(), response)
		conn.Write([]byte(response + "\n\n"))
	}
}

func policyGetResponseForMessage(message string, remoteAddr string) string {
	policyRequest, err := policyParseMessage(message)
	json, _ := json.Marshal(policyRequest)
	Log.Debug("Received new input from %s: %s", remoteAddr, json)
	if err != nil {
		Log.Warning(err.Error())
		return "action=defer_if_permit Policy Service is unavailable. Please try again or contact support"
	}

	response := moduleGetResponse(policyRequest)
	if response == "" {
		return "action=defer_if_permit Policy Service is unavailable. Please try again or contact support"
	}
	return "action=" + response
}

func policyParseMessage(message string) (map[string]string, error) {
	policyRequest := make(map[string]string)
	policyRequest["sender"] = ""
	policyRequest["count"] = "1"
	policyRequest["recipient"] = ""
	policyRequest["client_address"] = ""
	policyRequest["sasl_username"] = ""

	lines := strings.Split(message, "\n")
	for _, line := range lines {
		keyVal := strings.SplitN(line, "=", 2)
		if len(keyVal) != 2 {
			return nil, errors.New(fmt.Sprintf("Could not parse line. Line was: '%s'", line))
		}

		policyRequest[keyVal[0]] = keyVal[1]
	}

	return policyRequest, nil
}
