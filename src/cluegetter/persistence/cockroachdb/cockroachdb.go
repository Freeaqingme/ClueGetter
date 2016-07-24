// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package cockroachdb

import (
	"fmt"

	"database/sql"

	"cluegetter/core"
	"cluegetter/persistence/base"
	_ "github.com/lib/pq"
)

type db struct {
	*base.Db

	dsn  string
	conn *sql.DB
}

func Init(cg *core.Cluegetter) []db {
	out := make([]db, 0)
	for name, conf := range cg.Config.Cockroach_Db {
		if !conf.Enabled {
			continue
		}

		out = append(out, initDb(cg, name))
	}

	return out
}

func initDb(cg *core.Cluegetter, name string) db {
	conf := cg.Config.Cockroach_Db[name]
	db := db{
		Db:  base.NewDb(cg, name, "cockroachdb"),
		dsn: getDsn(conf, false),
	}

	var err error
	db.conn, err = sql.Open("postgres", db.dsn)
	if err != nil {
		cg.Log.Fatalf("Could not connect with cockroachdb '%s': %s", getDsn(conf, true), err.Error())
	}

	// TOOD: .Ping() is woefully insufficient, because even with invalid credentials
	// it still passes. Better think of some simple query that fails with invalid credentials.
	err = db.conn.Ping()
	if err != nil {
		cg.Log.Fatalf("Could not connect with %s: %s", getDsn(conf, true), err.Error())
	}

	cg.Log.Infof("Successfully connected with cockroachdb '%s'", name)
	return db
}

func getDsn(conf *core.ConfigCockroachDb, obfuscate bool) string {
	pass := conf.Password
	if obfuscate {
		pass = "***"
	}

	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		conf.User,
		pass,
		conf.Host,
		conf.Port,
		conf.Database,
		conf.SslMode,
	)
}
