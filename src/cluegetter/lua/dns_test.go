package lua

import (
	"testing"

	cluegetter "cluegetter"
)

func TestLuaDnsTxt(t *testing.T) {
	luaScript := `
local module = {}

local dns = require "dns"
local string = require "string"

module.milterCheck = function(message)
    res, err = dns.queryTxt("google-public-dns-a.google.com")
    if err ~= nil then
	return "TEMPFAIL", err, 100
    end

    for k,v in pairs(res) do
        if string.lower(v) ~= "" then
 		return "PERMIT", res[k], 0
        end
    end

    return "ERROR", res[1], 0
end

return module
	`

	testLuaMilterCheck(t, luaScript, "http://xkcd.com/1361/", 0, 0)
}

func TestLuaDnsCname(t *testing.T) {
	luaScript := `
local module = {}

local dns = require "dns"
local string = require "string"

module.milterCheck = function(message)
    res, err = dns.queryCname("www.golang.org")
    if err ~= nil then
	return "TEMPFAIL", err, 100
    end

    for k,v in pairs(res) do
        if string.lower(v) ~= "" then
 		return "PERMIT", res[k], 0
        end
    end

    return "ERROR", res[1], 0
end

return module
	`

	testLuaMilterCheck(t, luaScript, "golang.org.", 0, 0)
}

func testLuaMilterCheck(t *testing.T, luaScript, message string, action int, score float64) {
	config := cluegetter.GetNewConfig()
	config.LuaModule = make(map[string]*cluegetter.ConfigLuaModule)
	config.LuaModule["test"] = &cluegetter.ConfigLuaModule{
		Enabled:        true,
		ScriptContents: luaScript,
	}

	cluegetter.SetConfig(config)
	cluegetter.LuaStart()

	done := make(chan bool)
	res := cluegetter.LuaMilterCheck("test", &cluegetter.Message{}, done)
	cluegetter.LuaReset()
	cluegetter.DaemonReset()

	if res.Module() != "lua-test" {
		t.Fatal("Expected module name 'lua-test', but got:", res.Module())
	}

	if res.SuggestedAction() != action {
		t.Fatalf("Expected suggested action '%d', but got: %d", action, res.SuggestedAction())
	}

	if res.Message() != message {
		t.Fatalf("Expected message '%s', but got: %s", message, res.Message())
	}

	if res.Score() != score {
		t.Fatalf("Expected score '%f', but got: %f", score, res.Score())
	}
}
