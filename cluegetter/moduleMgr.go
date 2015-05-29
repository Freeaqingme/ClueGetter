// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"database/sql"
)

var ModuleInsertMessageStmt = *new(*sql.Stmt)

func moduleMgrStart() {
	stmt, err := Rdbms.Prepare(`
		INSERT INTO message (id, date, count, last_protocol_state, sender, recipient, client_address, sasl_username)
		VALUES (?, now(), ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY
		UPDATE count=?, last_protocol_state=?, sender=?, recipient=?, client_address=?, sasl_username=?`)
	if err != nil {
		Log.Fatal(err)
	}
	ModuleInsertMessageStmt = stmt

	quotasStart()

	Log.Info("Module Manager started successfully")
}

func moduleMgrStop() {
	quotasStop()

	ModuleInsertMessageStmt.Close()
	Log.Info("Module Manager ended")
}

func moduleMgrGetResponse(policyRequest map[string]string) string {
	if _, ok := policyRequest["instance"]; !ok {
		Log.Warning("No instance value specified")
		return ""
	} else if _, ok := policyRequest["protocol_state"]; !ok {
		Log.Warning("No protocol_state value specified")
		return ""
	}

	StatsCounters["RdbmsQueries"].increase(1)
	_, err := ModuleInsertMessageStmt.Exec(
		policyRequest["instance"], policyRequest["count"], policyRequest["protocol_state"],
		policyRequest["sender"], policyRequest["recipient"], policyRequest["client_address"], policyRequest["sasl_username"],
		policyRequest["count"], policyRequest["protocol_state"],
		policyRequest["sender"], policyRequest["recipient"], policyRequest["client_address"], policyRequest["sasl_username"],
	)
	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
	}

	if Config.Quotas.Enabled {
		return quotasIsAllowed(policyRequest)
	}

	return "action=dunno"
}
