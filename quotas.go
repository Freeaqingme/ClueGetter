// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	_ "github.com/go-sql-driver/mysql"
)

func quotasIsAllowed(policyRequest map[string]string) string {
	return "maybe"
}
