// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package spf

import (
	spf "github.com/Freeaqingme/go-libspf2"
	"github.com/yuin/gopher-lua"

	"net"
)

var spfClient = spf.NewClient()

func SpfLoader(L *lua.LState) int {
	mod := L.SetFuncs(L.NewTable(), exports)
	L.Push(mod)
	return 1
}

var exports = map[string]lua.LGFunction{
	"query": spfQuery,
}

func spfQuery(L *lua.LState) int {
	domain := L.ToString(1)
	ip := net.ParseIP(L.ToString(2))
	if ip == nil {
		L.Push(lua.LString(""))
		L.Push(lua.LString("Could not parse IP"))
		return 2
	}

	res, err := spfClient.Query(domain, ip)
	if err != nil {
		L.Push(lua.LString(""))
		L.Push(lua.LString("Error while performing SFP lookup: " + err.Error()))
		return 2
	}

	L.Push(lua.LString(res.String()))
	return 1
}
