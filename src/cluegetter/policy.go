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

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

var PolicyListener net.Listener

func PolicyStart() {
	// ClueGetter initially started off as a policy daemon. However, shortly
	// after it was decided to change it into a milter daemon instead. As such
	// this code is currenlty not used.
	// It is left in place though because in the future, we may provide both
	// milter and policy support (where either one can be used).
	return

	listen_host := Config.ClueGetter.Policy_Listen_Host
	listen_port := Config.ClueGetter.Policy_Listen_Port

	StatsCounters["PolicyConnections"] = &StatsCounter{ignore_prune: true}
	StatsCounters["PolicyProtocolErrors"] = &StatsCounter{}
	StatsCounters["PolicySocketErrors"] = &StatsCounter{}

	l, err := net.Listen("tcp", listen_host+":"+listen_port)
	if nil != err {
		Log.Fatal(err)
	}

	go policyWaitForConnections(l)
	PolicyListener = l
	Log.Notice("Now listening on " + listen_host + ":" + listen_port)
}

func PolicyStop() {
	return

	// These values could perhaps also be retrieved from the listener?
	listen_host := Config.ClueGetter.Policy_Listen_Host
	listen_port := Config.ClueGetter.Policy_Listen_Port

	PolicyListener.Close()
	Log.Notice("Policy module stopped listening on " + listen_host + ":" + listen_port)
}

func policyWaitForConnections(l net.Listener) {
	backoffTime := float32(0)

	for {
		conn, err := l.Accept()
		if err != nil && backoffTime < 8000 {
			StatsCounters["PolicySocketErrors"].increase(1)
			backoffTime = (backoffTime * 1.02) + 250
			Log.Error("Could not accept connection: %s. Backing off for %d ms",
				err, int(backoffTime))
			time.Sleep(time.Duration(backoffTime) * time.Millisecond)
			break
		} else if err != nil {
			StatsCounters["PolicySocketErrors"].increase(1)
			Log.Fatal("Could not accept new connection. Backing out: %s", err)
		}

		backoffTime = backoffTime / 1.1
		go policyHandleConnection(conn)
	}
}

func policyHandleConnection(conn net.Conn) {
	defer conn.Close()
	StatsCounters["PolicyConnections"].increase(1)
	defer StatsCounters["PolicyConnections"].decrease(1)

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
			StatsCounters["PolicyProtocolErrors"].increase(1)
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
		StatsCounters["PolicyProtocolErrors"].increase(1)
		Log.Warning(err.Error())
		return "action=defer_if_permit Policy Service is unavailable. Please try again or contact support"
	}

	response := ""
	//	response := moduleMgrGetResponse(policyRequest) // TODO
	if response == "" {
		StatsCounters["PolicyProtocolErrors"].increase(1)
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
