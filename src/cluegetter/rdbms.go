// ClueGetter - Does things with mail
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
	"database/sql"
	"fmt"
	_ "github.com/Freeaqingme/golang-sql-driver-mysql"
)

var Rdbms *sql.DB

func rdbmsStart() {
	dsn := rdbmsGetDsn(false)
	display_dsn := rdbmsGetDsn(true)

	err_msg := "Could not connect to %s. Got error: %s"
	rdbms, err := sql.Open(Config.ClueGetter.Rdbms_Driver, dsn)
	if err != nil {
		Log.Fatal(fmt.Sprintf(err_msg, display_dsn, err))
	}
	Rdbms = rdbms

	err = Rdbms.Ping()
	if err != nil {
		Log.Fatal(fmt.Sprintf(err_msg, display_dsn, err))
	}

	statsInitCounter("RdbmsQueries")
	statsInitCounter("RdbmsErrors")

	var version string
	Rdbms.QueryRow("SELECT VERSION()").Scan(&version)
	Log.Info(fmt.Sprintf("Successfully connected to %s: %s", display_dsn, version))
}

func rdbmsStop() {
	Rdbms.Close()
	Log.Info("Disconnected from RDBMS %s", rdbmsGetDsn(true))
}

func rdbmsGetDsn(display bool) string {
	cfg := Config.ClueGetter
	dsn_options := "sql_notes=false&parseTime=true&SESSION tx_isolation='READ-UNCOMMITTED'"
	if cfg.Rdbms_Mysql_Strictmode {
		dsn_options += "&strict=true"
	}

	password := cfg.Rdbms_Password
	if display && cfg.Rdbms_Password != "" {
		password = "***"
	}

	return fmt.Sprintf("%s:%s@%s(%s)/%s?%s",
		cfg.Rdbms_User, password, cfg.Rdbms_Protocol,
		cfg.Rdbms_Address, cfg.Rdbms_Database, dsn_options)
}
