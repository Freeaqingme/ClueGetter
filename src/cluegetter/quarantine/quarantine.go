// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package quarantine

import (
	"fmt"
	"os"
	"crypto/md5"
	"io/ioutil"
	"time"
	"strconv"

	"cluegetter/core"
)

const ModuleName = "quarantine"

type module struct {
	*core.BaseModule
}

func init() {
	core.ModuleRegister(&module{
		BaseModule: core.NewBaseModule(nil),
	})
}

func (m *module) Name() string {
	return ModuleName
}

func (m *module) Enable() bool {
	return m.Config().Quarantine.Enabled
}

func (m *module) Init() {
}

func (m *module) SessionDisconnect(sess *core.MilterSession) {
	m.persistSession(sess)
}

func (m *module) persistSession(sess *core.MilterSession) {
	if sess.ClientIsMonitorHost() && len(sess.Messages) == 0 {
		return
	}

	for _, msg := range sess.Messages {
		m.persistMessage(sess, msg)
	}
}

func (m *module) persistMessage(sess *core.MilterSession, msg *core.Message) {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(msg.QueueId)))

	varDir := m.Cluegetter.Config().ClueGetter.Var_Dir
	path := varDir + "/quarantine/" + string(hash[0]) + "/" + string(hash[1])+ "/" + string(hash[2])
	os.MkdirAll(path,0700)

	contents, _ := sess.MarshalJSONWithSingleMessage(msg, true)
	err := ioutil.WriteFile(path + "/" + msg.QueueId + "," + strconv.Itoa(int(time.Now().Add(7 * 24 * time.Hour).Unix())) + ".clueg", contents, 0600)
	if err != nil {
		fmt.Println("Error!!!", err.Error())
	}
}
