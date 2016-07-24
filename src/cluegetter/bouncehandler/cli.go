// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package bounceHandler

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"

	"cluegetter/core"

	"github.com/Freeaqingme/GoDaemonSkeleton"
)

func init() {
	submitCli := bounceHandlerSubmitCli
	GoDaemonSkeleton.AppRegister(&GoDaemonSkeleton.App{
		Name:     ModuleName,
		Handover: &submitCli,
	})
}

// Submit a new report to the bounce handler through the CLI.
func bounceHandlerSubmitCli() {
	if len(os.Args) < 2 || os.Args[1] != "submit" {
		fmt.Println("Missing argument for 'bouncehandler'. Must be one of: submit")
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	body := make([]byte, 0)
	for {
		buf := make([]byte, 512)
		nr, err := reader.Read(buf)
		if err != nil {
			break
		}
		body = append(body, buf[:nr]...)
	}

	bodyB64 := base64.StdEncoding.EncodeToString(body)
	core.DaemonIpcSend("bouncehandler!submit", bodyB64)
}
