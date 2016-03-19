// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	cg_lua "cluegetter/lua"
	"github.com/yuin/gopher-lua"

	"io/ioutil"
)

var luaModules = make(map[string]string, 0)

func init() {
	init := luaStart

	ModuleRegister(&module{
		name: "lua",
		init: &init,
	})
}

func luaStart() {
	for name, conf := range Config.LuaModule {
		luaStartModule(name, conf)
	}
}

func luaStartModule(name string, conf *ConfigLuaModule) {
	enable := func() bool { return conf.Enabled }
	milterCheck := func(msg *Message, done chan bool) *MessageCheckResult {
		return luaMilterCheck(name, msg, done)
	}

	script, err := ioutil.ReadFile(conf.Script)
	if err != nil {
		panic("Could not load LUA script: " + err.Error())
	}
	if _, err := luaCanParse(string(script)); err != nil {
		panic("Could not parse LUA module '" + name + "': " + err.Error())
	}
	luaModules[name] = string(script)

	ModuleRegister(&module{
		name:        "lua-" + name,
		enable:      &enable,
		milterCheck: &milterCheck,
	})
}

func luaMilterCheck(luaModuleName string, msg *Message, done chan bool) *MessageCheckResult {
	L := luaGetState()

	if err := L.DoString(luaModules[luaModuleName]); err != nil {
		panic("Could not execute lua module " + luaModuleName + ": " + err.Error())
	}

	err := L.CallByParam(lua.P{
		Fn:      L.GetGlobal("milterCheck"),
		NRet:    3,
		Protect: true,
	}, luaGetMessage(L, msg))
	if err != nil {
		panic(err)
	}
	resScore := L.Get(-1)
	L.Pop(1)
	resMsg := L.Get(-1)
	L.Pop(1)
	suggestedActionStr := L.Get(-1)
	L.Pop(1)

	var suggestedAction int32
	var ok bool
	if suggestedAction, ok = Proto_MessageV1_Verdict_value[suggestedActionStr.String()]; !ok {
		panic("Invalid suggested action from lua module '" + luaModuleName + "': " + suggestedActionStr.String())
	}

	return &MessageCheckResult{
		module:          "lua-" + luaModuleName,
		suggestedAction: int(suggestedAction),
		message:         resMsg.String(),
		score:           float64(lua.LVAsNumber(resScore)),
	}
}

func luaGetState() *lua.LState {
	L := lua.NewState()
	defer L.Close()

	L.PreloadModule("spf", cg_lua.SpfLoader)

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

func luaMessageFuncQueueId(L *lua.LState) int {
	p := luaMessageGetFromVM(L)
	L.Push(lua.LString(p.QueueId))
	return 1
}

func luaMessageFuncFrom(L *lua.LState) int {
	p := luaMessageGetFromVM(L)
	L.Push(lua.LString(p.From))
	return 1
}

func luaMessageFuncRcpt(L *lua.LState) int {
	p := luaMessageGetFromVM(L)

	t := L.NewTable()
	for _, v := range p.Rcpt {
		t.Append(lua.LString(v))
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
		ud.Value = v
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
