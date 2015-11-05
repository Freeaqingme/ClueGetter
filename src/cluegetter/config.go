// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"github.com/scalingdata/gcfg"
)

type config struct {
	ClueGetter struct {
		Instance                         string
		Noop                             bool
		Exit_On_Panic                    bool
		Policy_Listen_Port               string
		Policy_Listen_Host               string
		Rdbms_Driver                     string
		Rdbms_User                       string
		Rdbms_Address                    string
		Rdbms_Password                   string
		Rdbms_Protocol                   string
		Rdbms_Database                   string
		Rdbms_Mysql_Strictmode           bool
		Message_Reject_Score             float64
		Message_Tempfail_Score           float64
		Breaker_Score                    float64
		Milter_Socket                    string
		Whitelist                        []string
		Add_Header                       []string
		Add_Header_X_Spam_Score          bool
		Insert_Missing_Message_Id        bool
		Archive_Retention_Cassandra      float64
		Archive_Prune_Interval           int
		Archive_Retention_Body           float64
		Archive_Retention_Header         float64
		Archive_Retention_Message_Result float64
		Archive_Retention_Message        float64
		Archive_Retention_Safeguard      float64
	}
	Cassandra struct {
		Enabled  bool
		Host     []string
		Keyspace string
		Username string
		Password string
	}
	ModuleGroup map[string]*struct {
		Module []string
	}
	Http struct {
		Enabled     bool
		Listen_Port string
		Listen_Host string
	}
	BounceHandler struct {
		Enabled     bool
		Listen_Port string
		Listen_Host string
	}
	Greylisting struct {
		Enabled        bool
		Initial_Score  float64
		Initial_Period uint16
		Whitelist_Spf  []string
	}
	Quotas struct {
		Enabled                bool
		Account_Sender         bool
		Account_Recipient      bool
		Account_Client_Address bool
		Account_Sasl_Username  bool
	}
	Rspamd struct {
		Enabled    bool
		Host       string
		Port       int
		Multiplier float64
	}
	SpamAssassin struct {
		Enabled  bool
		Host     string
		Port     int
		Max_Size int
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
	cfg.ClueGetter.Noop = false
	cfg.ClueGetter.Exit_On_Panic = false
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
	cfg.ClueGetter.Breaker_Score = 100
	cfg.ClueGetter.Milter_Socket = "inet:10033@127.0.0.1"
	cfg.ClueGetter.Whitelist = []string{} // "127.0.0.0/8", "::1" }
	cfg.ClueGetter.Add_Header = []string{}
	cfg.ClueGetter.Add_Header_X_Spam_Score = true
	cfg.ClueGetter.Insert_Missing_Message_Id = true
	cfg.ClueGetter.Archive_Retention_Cassandra = 4
	cfg.ClueGetter.Archive_Prune_Interval = 21600
	cfg.ClueGetter.Archive_Retention_Safeguard = 1.01
	cfg.ClueGetter.Archive_Retention_Body = 2
	cfg.ClueGetter.Archive_Retention_Header = 26
	cfg.ClueGetter.Archive_Retention_Message_Result = 2
	cfg.ClueGetter.Archive_Retention_Message = 52

	cfg.Cassandra.Enabled = false
	cfg.Cassandra.Keyspace = "cluegetter"
	cfg.Cassandra.Host = []string{}
	cfg.Cassandra.Username = ""
	cfg.Cassandra.Password = ""

	cfg.Http.Enabled = true
	cfg.Http.Listen_Port = "1937"
	cfg.Http.Listen_Host = "127.0.0.1"

	cfg.BounceHandler.Enabled = false
	cfg.BounceHandler.Listen_Port = "10034"
	cfg.BounceHandler.Listen_Host = "127.0.0.1"

	cfg.Greylisting.Enabled = false
	cfg.Greylisting.Initial_Score = 7.0
	cfg.Greylisting.Initial_Period = 5
	cfg.Greylisting.Whitelist_Spf = []string{}

	cfg.Quotas.Enabled = false
	cfg.Quotas.Account_Client_Address = true
	cfg.Quotas.Account_Sender = false
	cfg.Quotas.Account_Recipient = false
	cfg.Quotas.Account_Sasl_Username = false

	cfg.Rspamd.Enabled = false
	cfg.Rspamd.Host = "127.0.0.1"
	cfg.Rspamd.Port = 11333
	cfg.Rspamd.Multiplier = 0.67

	cfg.SpamAssassin.Enabled = false
	cfg.SpamAssassin.Host = "127.0.0.1"
	cfg.SpamAssassin.Port = 783
	cfg.SpamAssassin.Max_Size = 8388608
}
