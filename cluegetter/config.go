// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"code.google.com/p/gcfg"
)

type config struct {
	ClueGetter struct {
		Stats_Listen_Port      string
		Stats_Listen_Host      string
		Rdbms_Driver           string
		Rdbms_User             string
		Rdbms_Address          string
		Rdbms_Password         string
		Rdbms_Protocol         string
		Rdbms_Database         string
		Rdbms_Mysql_Strictmode bool
	}
	Quotas struct {
		Enabled                bool
		Account_Sender         bool
		Account_Recipient      bool
		Account_Client_Address bool
		Account_Sasl_Username  bool
	}
}

func LoadConfig(cfgFile string, cfg *config) {
	err := gcfg.ReadFileInto(cfg, cfgFile)

	if err != nil {
		Log.Fatal("Couldnt read config file: " + err.Error())
	}
}

func DefaultConfig(cfg *config) {
	cfg.ClueGetter.Stats_Listen_Port = "10032"
	cfg.ClueGetter.Stats_Listen_Host = "0.0.0.0"
	cfg.ClueGetter.Rdbms_Driver = "mysql"
	cfg.ClueGetter.Rdbms_User = "root"
	cfg.ClueGetter.Rdbms_Address = "localhost:3306"
	cfg.ClueGetter.Rdbms_Password = ""
	cfg.ClueGetter.Rdbms_Protocol = "tcp"
	cfg.ClueGetter.Rdbms_Database = "cluegetter"
	cfg.ClueGetter.Rdbms_Mysql_Strictmode = true

	cfg.Quotas.Enabled = false
	cfg.Quotas.Account_Sender = true
	cfg.Quotas.Account_Recipient = true
	cfg.Quotas.Account_Client_Address = true
	cfg.Quotas.Account_Sasl_Username = true
}
