// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"github.com/scalingdata/gcfg"
)

type config struct {
	ClueGetter struct {
		IPC_Socket                       string
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
		Message_Reject_Score             float64
		Message_Tempfail_Score           float64
		Message_Spamflag_Score           float64
		Breaker_Score                    float64
		Milter_Socket                    string
		Whitelist                        []string
		Add_Header                       []string
		Add_Header_X_Spam_Score          bool
		Insert_Missing_Message_Id        bool
		Archive_Prune_Interval           int
		Archive_Retention_Body           float64
		Archive_Retention_Header         float64
		Archive_Retention_Message_Result float64
		Archive_Retention_Message        float64
		Archive_Retention_Safeguard      float64
		Message_Cache_Size               int
		Monitor_Host                     []string
	}
	Redis struct {
		Enabled  bool
		Host     []string
		Method   string
		Dump_Dir string
		Dump_Key []string
	}
	ModuleGroup   map[string]*ConfigModuleGroup
	Elasticsearch struct {
		Enabled bool
		Sniff   bool
		Url     []string
	}
	Cockroach_Db map[string]*ConfigCockroachDb
	Http         struct {
		Enabled          bool
		Listen_Port      string
		Listen_Host      string
		Google_Analytics string
	}
	HttpFrontend  map[string]*ConfigHttpFrontend
	LuaModule     map[string]*ConfigLuaModule
	BounceHandler struct {
		Enabled     bool
		Listen_Port string
		Listen_Host string
		Dump_Dir    string
	}
	Bayes struct {
		Enabled bool
	}
	MailQueue struct {
		Enabled             bool
		Spool_Dir           string
		Update_Interval     int
		PostsuperExecutable string
		PostcatExecutable   string
	}
	Srs struct {
		Enabled          bool
		Recipient_Header string
	}
	Clamav struct {
		Enabled       bool
		Address       string
		Default_Score float64
		Max_Size      int
	}
	Contacts struct {
		Enabled                 bool
		Whitelist_Address_Score float64
		Whitelist_Domain_Score  float64
		Blacklist_Address_Score float64
		Blacklist_Domain_Score  float64
	}
	Dkim struct {
		Enabled         bool
		Sign            string
		Selector        []string
		Backend         string
		Sign_Headers    []string
		Sign_Bodylength uint
	}
	Dkim_FileBackend struct {
		Key_Path []string
	}
	Greylisting struct {
		Enabled        bool
		Initial_Score  float64
		Initial_Period uint16
		Whitelist_Spf  []string
	}
	Quotas struct {
		Enabled                  bool
		Account_Sender           bool
		Account_Sender_Domain    bool
		Account_Recipient        bool
		Account_Recipient_Domain bool
		Account_Client_Address   bool
		Account_Sasl_Username    bool
	}
	Rspamd struct {
		Enabled    bool
		Host       string
		Port       int
		Multiplier float64
	}
	SpamAssassin struct {
		Enabled         bool
		Host            string
		Port            int
		Timeout         float64
		Connect_Timeout float64
		Max_Size        int
		Verdict_Msg     []string
	}
}

type ConfigHttpFrontend struct {
	Enabled               bool
	Listen_Port           string
	Listen_Host           string
	Enable_Proxy_Protocol bool
}

type ConfigLuaModule struct {
	Enabled        bool
	Script         string
	ScriptContents string
}

type ConfigCockroachDb struct {
	Enabled  bool
	Database string
	User     string
	Password string
	Host     string
	Port     int
	SslMode  string
}

type SessionConfig struct {
	ClueGetter struct {
		Message_Reject_Score      float64
		Message_Tempfail_Score    float64
		Message_Spamflag_Score    float64
		Breaker_Score             float64
		Insert_Missing_Message_Id bool
	}
	Clamav struct {
		Enabled       bool
		Default_Score float64
		Max_Size      int
	}
	Contacts struct {
		Enabled                 bool
		Whitelist_Address_Score float64
		Whitelist_Domain_Score  float64
		Blacklist_Address_Score float64
		Blacklist_Domain_Score  float64
	}
	Dkim struct {
		Sign     string
		Selector []string
	}
	Greylisting struct {
		Enabled        bool
		Initial_Score  float64
		Initial_Period uint16
		Whitelist_Spf  []string
	}
	Quotas struct {
		Enabled bool
	}
	Rspamd struct {
		Enabled    bool
		Multiplier float64
	}
	SpamAssassin struct {
		Enabled         bool
		Timeout         float64
		Connect_Timeout float64
		Max_Size        int
	}
}

type ConfigModuleGroup struct {
	Module []string
}

func (conf *config) sessionConfig() (sconf *SessionConfig) {
	sconf = &SessionConfig{}
	sconf.ClueGetter.Message_Reject_Score = conf.ClueGetter.Message_Reject_Score
	sconf.ClueGetter.Message_Tempfail_Score = conf.ClueGetter.Message_Tempfail_Score
	sconf.ClueGetter.Message_Spamflag_Score = conf.ClueGetter.Message_Spamflag_Score
	sconf.ClueGetter.Breaker_Score = conf.ClueGetter.Breaker_Score
	sconf.ClueGetter.Insert_Missing_Message_Id = conf.ClueGetter.Insert_Missing_Message_Id

	sconf.Clamav.Enabled = conf.Clamav.Enabled
	sconf.Clamav.Default_Score = conf.Clamav.Default_Score
	sconf.Clamav.Max_Size = conf.Clamav.Max_Size

	sconf.Contacts.Enabled = conf.Contacts.Enabled
	sconf.Contacts.Whitelist_Address_Score = conf.Contacts.Whitelist_Address_Score
	sconf.Contacts.Whitelist_Domain_Score = conf.Contacts.Whitelist_Domain_Score
	sconf.Contacts.Blacklist_Address_Score = conf.Contacts.Blacklist_Address_Score
	sconf.Contacts.Blacklist_Domain_Score = conf.Contacts.Blacklist_Domain_Score
	sconf.Contacts.Whitelist_Address_Score = conf.Contacts.Whitelist_Address_Score
	sconf.Contacts.Whitelist_Domain_Score = conf.Contacts.Whitelist_Domain_Score

	sconf.Dkim.Sign = conf.Dkim.Sign
	sconf.Dkim.Selector = conf.Dkim.Selector

	sconf.Greylisting.Enabled = conf.Greylisting.Enabled
	sconf.Greylisting.Initial_Score = conf.Greylisting.Initial_Score
	sconf.Greylisting.Initial_Period = conf.Greylisting.Initial_Period
	sconf.Greylisting.Whitelist_Spf = conf.Greylisting.Whitelist_Spf

	sconf.Quotas.Enabled = conf.Quotas.Enabled

	sconf.Rspamd.Enabled = conf.Rspamd.Enabled
	sconf.Rspamd.Multiplier = conf.Rspamd.Multiplier

	sconf.SpamAssassin.Enabled = conf.SpamAssassin.Enabled
	sconf.SpamAssassin.Timeout = conf.SpamAssassin.Timeout
	sconf.SpamAssassin.Connect_Timeout = conf.SpamAssassin.Connect_Timeout
	sconf.SpamAssassin.Max_Size = conf.SpamAssassin.Max_Size

	return
}

func LoadConfig(cfgFile string, cfg *config) {
	err := gcfg.ReadFileInto(cfg, cfgFile)

	if err != nil {
		Log.Fatalf("Couldnt read config file: " + err.Error())
	}
}

func DefaultConfig(cfg *config) {
	cfg.ClueGetter.IPC_Socket = "/var/run/cluegetter/ipc.sock"
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
	cfg.ClueGetter.Message_Reject_Score = 5
	cfg.ClueGetter.Message_Tempfail_Score = 8
	cfg.ClueGetter.Message_Spamflag_Score = 4.5
	cfg.ClueGetter.Breaker_Score = 100
	cfg.ClueGetter.Milter_Socket = "inet:10033@127.0.0.1"
	cfg.ClueGetter.Whitelist = []string{} // "127.0.0.0/8", "::1" }
	cfg.ClueGetter.Add_Header = []string{}
	cfg.ClueGetter.Insert_Missing_Message_Id = true
	cfg.ClueGetter.Archive_Prune_Interval = 21600
	cfg.ClueGetter.Archive_Retention_Safeguard = 1.01
	cfg.ClueGetter.Archive_Retention_Body = 2
	cfg.ClueGetter.Archive_Retention_Header = 26
	cfg.ClueGetter.Archive_Retention_Message_Result = 2
	cfg.ClueGetter.Archive_Retention_Message = 52
	cfg.ClueGetter.Message_Cache_Size = 256
	cfg.ClueGetter.Monitor_Host = []string{}

	cfg.Redis.Host = []string{}
	cfg.Redis.Method = "standalone"
	cfg.Redis.Dump_Key = []string{}

	cfg.Elasticsearch.Sniff = true
	cfg.Elasticsearch.Url = []string{}

	cfg.Http.Enabled = true
	cfg.Http.Listen_Port = "0"
	cfg.Http.Listen_Host = "127.0.0.1"
	cfg.Http.Google_Analytics = ""

	cfg.BounceHandler.Listen_Port = "10034"
	cfg.BounceHandler.Listen_Host = "127.0.0.1"

	cfg.MailQueue.Spool_Dir = "/var/spool/postfix"
	cfg.MailQueue.Update_Interval = 5

	cfg.Srs.Recipient_Header = "X-Original-To"

	cfg.Clamav.Default_Score = 10.0
	cfg.Clamav.Max_Size = 10485760

	cfg.Contacts.Whitelist_Address_Score = -2.5
	cfg.Contacts.Whitelist_Domain_Score = -0.85
	cfg.Contacts.Blacklist_Address_Score = 6
	cfg.Contacts.Blacklist_Domain_Score = 1

	cfg.Dkim.Sign = "optional"
	cfg.Dkim.Selector = []string{}
	cfg.Dkim.Backend = "file"
	cfg.Dkim_FileBackend.Key_Path = []string{}
	cfg.Dkim.Sign_Headers = []string{}

	cfg.Greylisting.Initial_Score = 7.0
	cfg.Greylisting.Initial_Period = 5
	cfg.Greylisting.Whitelist_Spf = []string{}

	cfg.Quotas.Account_Client_Address = true
	cfg.Quotas.Account_Sender = false
	cfg.Quotas.Account_Sender_Domain = false
	cfg.Quotas.Account_Recipient = false
	cfg.Quotas.Account_Recipient_Domain = false
	cfg.Quotas.Account_Sasl_Username = false

	cfg.Rspamd.Host = "127.0.0.1"
	cfg.Rspamd.Port = 11333
	cfg.Rspamd.Multiplier = 0.67

	cfg.SpamAssassin.Host = "127.0.0.1"
	cfg.SpamAssassin.Port = 783
	cfg.SpamAssassin.Timeout = 10
	cfg.SpamAssassin.Connect_Timeout = 0.1
	cfg.SpamAssassin.Max_Size = 500000 // Default SA max file size: 512 KB
	cfg.SpamAssassin.Verdict_Msg = []string{}
}

func GetNewConfig() *config {
	out := &config{}
	DefaultConfig(out)

	return out
}

func SetConfig(config *config) {
	Config = *config
}
