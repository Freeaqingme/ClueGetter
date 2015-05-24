// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"code.google.com/p/gcfg"
	"log"
)

type config struct {
	ClueGetter struct {
		Stats_Listen_Port string
		Stats_Listen_Host string
	}
}

func LoadConfig(cfgFile string, cfg *config) {
	err := gcfg.ReadFileInto(cfg, cfgFile)

	if err != nil {
		log.Fatalln("Couldnt read config file: " + err.Error())
	}

}

func DefaultConfig(cfg *config) {
	cfg.ClueGetter.Stats_Listen_Port = "10032"
	cfg.ClueGetter.Stats_Listen_Host = "0.0.0.0"
}
