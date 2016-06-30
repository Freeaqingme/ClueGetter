// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"

	_ "github.com/Freeaqingme/golang-sql-driver-mysql"
)

var Rdbms *sql.DB

func rdbmsStart() {
	dsn := rdbmsGetDsn(false)
	display_dsn := rdbmsGetDsn(true)

	err_msg := "Could not connect to %s. Got error: %s"
	rdbms, err := sql.Open(Config.ClueGetter.Rdbms_Driver, dsn)
	if err != nil {
		Log.Fatalf(fmt.Sprintf(err_msg, display_dsn, err))
	}
	Rdbms = rdbms

	err = Rdbms.Ping()
	if err != nil {
		Log.Fatalf(fmt.Sprintf(err_msg, display_dsn, err))
	}

	statsInitCounter("RdbmsQueries")
	statsInitCounter("RdbmsErrors")

	var version string
	Rdbms.QueryRow("SELECT VERSION()").Scan(&version)
	Log.Infof(fmt.Sprintf("Successfully connected to %s: %s", display_dsn, version))
}

func rdbmsStop() {
	Rdbms.Close()
	Log.Infof("Disconnected from RDBMS %s", rdbmsGetDsn(true))
}

func rdbmsGetDsn(display bool) string {
	cfg := Config.ClueGetter
	dsn_options := "sql_notes=false&parseTime=true&strict=true&SESSION tx_isolation='READ-UNCOMMITTED'"

	password := cfg.Rdbms_Password
	if display && cfg.Rdbms_Password != "" {
		password = "***"
	}

	return fmt.Sprintf("%s:%s@%s(%s)/%s?%s",
		cfg.Rdbms_User, password, cfg.Rdbms_Protocol,
		cfg.Rdbms_Address, cfg.Rdbms_Database, dsn_options)
}

// TODO: Make method of RdbmsClient
func RdbmsRowsInTable(table string) (count int) {
	err := Rdbms.QueryRow(`
		SELECT TABLE_ROWS FROM information_schema.tables
			WHERE TABLE_SCHEMA = database() AND TABLE_NAME = ?
	`, table).Scan(&count)
	if err != nil {
		Log.Fatalf("%s", err)
	}

	return count
}

type NullTime struct {
	time.Time
	Valid bool // Valid is true if Time is not NULL
}

// Scan implements the Scanner interface.
func (nt *NullTime) Scan(value interface{}) error {
	nt.Time, nt.Valid = value.(time.Time)
	return nil
}

// Value implements the driver Valuer interface.
func (nt NullTime) Value() (driver.Value, error) {
	if !nt.Valid {
		return nil, nil
	}
	return nt.Time, nil
}
