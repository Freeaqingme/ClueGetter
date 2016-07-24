// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"io/ioutil"
	"strings"

	cg_lua "cluegetter/lua"

	"github.com/yuin/gopher-lua"
)

var luaModules = make(map[string]string, 0)

func init() {
	init := LuaStart
	enable := func() bool { return true }

	ModuleRegister(&ModuleOld{
		name:   "lua",
		enable: &enable,
		init:   &init,
	})
}

func LuaStart() {
	for name, conf := range Config.LuaModule {
		luaStartModule(name, conf)
	}
}

func LuaReset() {
	luaModules = make(map[string]string, 0)
}

func luaStartModule(name string, conf *ConfigLuaModule) {
	enable := func() bool { return conf.Enabled }
	milterCheck := func(msg *Message, done chan bool) *MessageCheckResult {
		return LuaMilterCheck(name, msg, done)
	}
	sessConf := func(sess *MilterSession) {
		luaSessionConfigure(name, sess)
	}

	if conf.Script != "" && conf.ScriptContents != "" {
		panic("Cannot specify both Script as well as scriptContents in " + name)
	} else if conf.Script == "" && conf.ScriptContents == "" {
		panic("Either a Script or ScriptContents must be specified in " + name)
	}

	var scriptContents string
	if conf.Script != "" {
		scriptContentsBytes, err := ioutil.ReadFile(conf.Script)
		if err != nil {
			panic("Could not load LUA script: " + err.Error())
		}
		scriptContents = string(scriptContentsBytes)
	} else {
		scriptContents = conf.ScriptContents
	}

	if _, err := luaCanParse(string(scriptContents)); err != nil {
		panic("Could not parse LUA module '" + name + "': " + err.Error())
	}
	luaModules[name] = string(scriptContents)

	Log.Infof("Registered LUA module " + name)
	ModuleRegister(&ModuleOld{
		name:        "lua-" + name,
		enable:      &enable,
		milterCheck: &milterCheck,
		sessConfig:  &sessConf,
	})
}

func LuaMilterCheck(luaModuleName string, msg *Message, done chan bool) *MessageCheckResult {
	L := luaGetState()

	if err := L.DoString(luaModules[luaModuleName]); err != nil {
		panic("Could not execute lua module " + luaModuleName + ": " + err.Error())
	}

	callback := L.GetField(L.Get(-1), "milterCheck")
	if callback == nil {
		return nil
	}

	err := L.CallByParam(lua.P{
		Fn:      callback,
		NRet:    3,
		Protect: true,
	}, luaGetMessage(L, msg))
	if err != nil {
		panic("Error in lua module '" + luaModuleName + "': " + err.Error())
	}
	resScore := L.Get(-1)
	L.Pop(1)
	resMsg := L.Get(-1)
	L.Pop(1)
	suggestedActionStr := L.Get(-1)
	L.Pop(1)

	var suggestedAction int32
	var ok bool
	if suggestedAction, ok = Proto_Message_Verdict_value[suggestedActionStr.String()]; !ok {
		panic("Invalid suggested action from lua module '" + luaModuleName + "': " + suggestedActionStr.String())
	}

	return &MessageCheckResult{
		Module:          "lua-" + luaModuleName,
		SuggestedAction: int(suggestedAction),
		Message:         resMsg.String(),
		Score:           float64(lua.LVAsNumber(resScore)),
	}
}

func luaSessionConfigure(luaModuleName string, sess *MilterSession) {
	L := luaGetState()

	if err := L.DoString(luaModules[luaModuleName]); err != nil {
		panic("Could not execute lua module " + luaModuleName + ": " + err.Error())
	}

	callback := L.GetField(L.Get(-1), "sessionConfigure")
	if callback == nil {
		return
	}

	err := L.CallByParam(lua.P{
		Fn:      callback,
		NRet:    0,
		Protect: true,
	}, luaGetSession(L, sess))
	if err != nil {
		panic("Error in lua module '" + luaModuleName + "': " + err.Error())
	}
}

func luaGetState() *lua.LState {
	L := lua.NewState(lua.Options{
		IncludeGoStackTrace: true,
	})
	defer L.Close()

	L.PreloadModule("crypto", cg_lua.CryptoLoader)
	L.PreloadModule("dns", cg_lua.DnsLoader)
	L.PreloadModule("spf", cg_lua.SpfLoader)

	luaSessionRegisterType(L)
	luaMessageRegisterType(L)
	luaMessageHeaderRegisterType(L)

	return L
}

func luaCanParse(script string) (bool, error) {
	L := lua.NewState()
	defer L.Close()

	_, err := L.LoadString(script)
	return err == nil, err
}

//////////////////////
////// VM state //////
//////////////////////

/* Session */

func luaGetSession(L *lua.LState, sess *MilterSession) lua.LValue {
	ud := L.NewUserData()
	ud.Value = sess
	L.SetMetatable(ud, L.GetTypeMetatable("session"))
	L.Push(ud)

	return ud
}

func luaSessionRegisterType(L *lua.LState) {
	mt := L.NewTypeMetatable("session")
	L.SetGlobal("session", mt)
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), luaSessionMethods))
}

var luaSessionMethods = map[string]lua.LGFunction{
	"config": luaSessionFuncGetSetSessionConfig,

	"getSaslUsername":  luaSessionFuncSaslUsername,
	"getSaslMethod":    luaSessionFuncSaslMethod,
	"getCertIssuer":    luaSessionFuncCertIssuer,
	"getCertSubject":   luaSessionFuncCertSubject,
	"getCipherBits":    luaSessionFuncCipherBits,
	"getCipher":        luaSessionFuncCipher,
	"getTlsVersion":    luaSessionFuncTlsVersion,
	"getIp":            luaSessionFuncIp,
	"getReverseDns":    luaSessionFuncReverseDns,
	"getHostname":      luaSessionFuncHostname,
	"getHelo":          luaSessionFuncHelo,
	"getMtaHostName":   luaSessionFuncMtaDaemonName,
	"getMtaDaemonName": luaSessionFuncMtaDaemonName,
}

func luaSessionGetFromVM(L *lua.LState) *MilterSession {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*MilterSession); ok {
		return v
	}
	L.ArgError(1, "Session expected")
	return nil
}

var sessionConfigSetters = map[string]func(*SessionConfig, *lua.LState){
	"dkim.sign": func(c *SessionConfig, L *lua.LState) {
		c.Dkim.Sign = L.CheckString(3)
	},

	// Greylisting
	"greylisting.enabled": func(c *SessionConfig, L *lua.LState) {
		c.Greylisting.Enabled = L.CheckBool(3)
	},
}

var sessionConfigGetters = map[string]func(*SessionConfig) lua.LValue{
	"dkim.sign": func(c *SessionConfig) lua.LValue {
		return lua.LString(c.Dkim.Sign)
	},

	// Greylisting
	"greylisting.enabled": func(c *SessionConfig) lua.LValue {
		return lua.LBool(c.Greylisting.Enabled)
	},
}

func luaSessionFuncGetSetSessionConfig(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	sconf := s.Config()
	key := strings.ToLower(L.CheckString(2))
	setter, ok := sessionConfigSetters[key]
	getter, ok2 := sessionConfigGetters[key]
	if !ok || !ok2 { // ensure each getter also has a setter, and vice versa
		L.ArgError(1, "No value by name '"+key+"'")
		return 0
	}

	if L.GetTop() == 3 {
		setter(sconf, L)
		return 0
	}

	L.Push(getter(sconf))
	return 1
}

func luaSessionFuncSaslUsername(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.SaslUsername))
	return 1
}

func luaSessionFuncSaslMethod(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.SaslMethod))
	return 1
}

func luaSessionFuncCertIssuer(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.CertIssuer))
	return 1
}

func luaSessionFuncCertSubject(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.CertSubject))
	return 1
}

func luaSessionFuncCipherBits(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.CipherBits))
	return 1
}

func luaSessionFuncCipher(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.Cipher))
	return 1
}

func luaSessionFuncTlsVersion(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.TlsVersion))
	return 1
}

func luaSessionFuncIp(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.Ip))
	return 1
}

func luaSessionFuncReverseDns(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.ReverseDns))
	return 1
}

func luaSessionFuncHostname(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.Hostname))
	return 1
}

func luaSessionFuncHelo(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.Helo))
	return 1
}

func luaSessionFuncMtaHostName(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.MtaHostName))
	return 1
}

func luaSessionFuncMtaDaemonName(L *lua.LState) int {
	s := luaSessionGetFromVM(L)
	L.Push(lua.LString(s.MtaDaemonName))
	return 1
}

/* Message */

func luaGetMessage(L *lua.LState, msg *Message) lua.LValue {
	ud := L.NewUserData()
	ud.Value = msg
	L.SetMetatable(ud, L.GetTypeMetatable("message"))
	L.Push(ud)

	return ud
}

func luaMessageRegisterType(L *lua.LState) {
	mt := L.NewTypeMetatable("message")
	L.SetGlobal("message", mt)
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), luaMessageMethods))
}

var luaMessageMethods = map[string]lua.LGFunction{
	"getSession": luaMessageFuncSession,
	"getQueueId": luaMessageFuncQueueId,
	"getFrom":    luaMessageFuncFrom,
	"getRcpt":    luaMessageFuncRcpt,
	"getBody":    luaMessageFuncBody,
	"getHeaders": luaMessageFuncHeaders,
}

func luaMessageGetFromVM(L *lua.LState) *Message {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*Message); ok {
		return v
	}
	L.ArgError(1, "Message expected")
	return nil
}

func luaMessageFuncSession(L *lua.LState) int {
	m := luaMessageGetFromVM(L)
	L.Push(luaGetSession(L, m.Session()))
	return 1
}

func luaMessageFuncQueueId(L *lua.LState) int {
	p := luaMessageGetFromVM(L)
	L.Push(lua.LString(p.QueueId))
	return 1
}

func luaMessageFuncFrom(L *lua.LState) int {
	p := luaMessageGetFromVM(L)
	L.Push(lua.LString(p.From.String()))
	return 1
}

func luaMessageFuncRcpt(L *lua.LState) int {
	p := luaMessageGetFromVM(L)

	t := L.NewTable()
	for _, v := range p.Rcpt {
		t.Append(lua.LString(v.String()))
	}

	L.Push(t)
	return 1
}

func luaMessageFuncBody(L *lua.LState) int {
	p := luaMessageGetFromVM(L)
	L.Push(lua.LString(p.Body))
	return 1
}

func luaMessageFuncHeaders(L *lua.LState) int {
	p := luaMessageGetFromVM(L)

	t := L.NewTable()
	for _, v := range p.Headers {
		ud := L.NewUserData()
		ud.Value = &v
		L.SetMetatable(ud, L.GetTypeMetatable("messageHeader"))
		t.Append(ud)
	}

	L.Push(t)
	return 1
}

/* Message Header */

func luaMessageHeaderRegisterType(L *lua.LState) {
	mt := L.NewTypeMetatable("messageHeader")
	L.SetGlobal("messageHeader", mt)
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), luaMessageHeaderMethods))
}

var luaMessageHeaderMethods = map[string]lua.LGFunction{
	"getKey":   luaMessageHeaderFuncKey,
	"getValue": luaMessageHeaderFuncValue,
}

func luaMessageHeaderGetFromVM(L *lua.LState) *MessageHeader {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*MessageHeader); ok {
		return v
	}
	L.ArgError(1, "MessageHeader expected")
	return nil
}

func luaMessageHeaderFuncKey(L *lua.LState) int {
	p := luaMessageHeaderGetFromVM(L)
	L.Push(lua.LString(p.Key))
	return 1
}

func luaMessageHeaderFuncValue(L *lua.LState) int {
	p := luaMessageHeaderGetFromVM(L)
	L.Push(lua.LString(p.Value))
	return 1
}
