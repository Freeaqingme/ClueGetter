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
		Instance               string
		Stats_Http_Listen_Port string
		Stats_Http_Listen_Host string
		Policy_Listen_Port     string
		Policy_Listen_Host     string
		Rdbms_Driver           string
		Rdbms_User             string
		Rdbms_Address          string
		Rdbms_Password         string
		Rdbms_Protocol         string
		Rdbms_Database         string
		Rdbms_Mysql_Strictmode bool
		Message_Reject_Score   int
		Message_Tempfail_Score int
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
	cfg.ClueGetter.Instance = "default"
	cfg.ClueGetter.Stats_Http_Listen_Port = "1936"
	cfg.ClueGetter.Stats_Http_Listen_Host = "0.0.0.0"
	cfg.ClueGetter.Policy_Listen_Port = "10032"
	cfg.ClueGetter.Policy_Listen_Host = "0.0.0.0"
	cfg.ClueGetter.Rdbms_Driver = "mysql"
	cfg.ClueGetter.Rdbms_User = "root"
	cfg.ClueGetter.Rdbms_Address = "localhost:3306"
	cfg.ClueGetter.Rdbms_Password = ""
	cfg.ClueGetter.Rdbms_Protocol = "tcp"
	cfg.ClueGetter.Rdbms_Database = "cluegetter"
	cfg.ClueGetter.Rdbms_Mysql_Strictmode = true
	cfg.ClueGetter.Message_Reject_Score = 5
	cfg.ClueGetter.Message_Tempfail_Score = 8

	cfg.Quotas.Enabled = false
	cfg.Quotas.Account_Sender = true
	cfg.Quotas.Account_Recipient = true
	cfg.Quotas.Account_Client_Address = true
	cfg.Quotas.Account_Sasl_Username = true
}
