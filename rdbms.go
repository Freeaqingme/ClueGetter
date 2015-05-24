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
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"log"
)

var Rdbms *sql.DB

func rdbmsStart(c chan int) {
	dsn := rdbmsGetDsn(false)
	display_dsn := rdbmsGetDsn(true)

	err_msg := "Could not connect to %s. Got error: %s"
	rdbms, err := sql.Open(Config.ClueGetter.Rdbms_Driver, dsn)
	if err != nil {
		log.Fatalln(fmt.Sprintf(err_msg, display_dsn, err))
	}
	Rdbms = rdbms

	err = Rdbms.Ping()
	if err != nil {
		log.Fatalln(fmt.Sprintf(err_msg, display_dsn, err))
	}

	var version string
	Rdbms.QueryRow("SELECT VERSION()").Scan(&version)
	log.Println(fmt.Sprintf("Successfully connected to %s: %s", display_dsn, version))

	c <- 1 // Let parent know we've connected successfully
	<-c
	Rdbms.Close()
	log.Println(fmt.Sprintf("Discconnected from RDBMS %s", display_dsn))
	c <- 1
}

func rdbmsGetDsn(display bool) string {
	cfg := Config.ClueGetter
	dsn_options := ""
	if cfg.Rdbms_Mysql_Strictmode {
		dsn_options = "strict=true"
	}

	password := cfg.Rdbms_Password
	if display && cfg.Rdbms_Password != "" {
		password = "***"
	}

	return fmt.Sprintf("%s:%s@%s(%s)/%s?%s",
		cfg.Rdbms_User, password, cfg.Rdbms_Protocol,
		cfg.Rdbms_Address, cfg.Rdbms_Database, dsn_options)
}
