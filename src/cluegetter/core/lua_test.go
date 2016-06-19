package core

import (
	"testing"
)

func TestLuaMilterCheckPermit(t *testing.T) {
	luaScript := `
local module = {}

module.milterCheck = function(message)
    return "PERMIT", "Some reason", 1337
end

return module
	`

	testLuaMilterCheck(t, luaScript, "Some reason", 0, 1337)
}

func TestLuaMilterCheckReject(t *testing.T) {
	luaScript := `
local module = {}

module.milterCheck = function(message)
    return "REJECT", "", -1.5
end

return module
	`

	testLuaMilterCheck(t, luaScript, "", 2, -1.5)
}

func TestLuaMilterCheckTempfail(t *testing.T) {
	luaScript := `
local module = {}

module.milterCheck = function(message)
    return "TEMPFAIL", "fOOb4r", 100
end

return module
	`

	testLuaMilterCheck(t, luaScript, "fOOb4r", 1, 100)
}

func TestLuaMilterCheckError(t *testing.T) {
	luaScript := `
local module = {}

module.milterCheck = function(message)
    return "ERROR", "", -25.4
end

return module
	`

	testLuaMilterCheck(t, luaScript, "", 3, -25.4)
}

func testLuaMilterCheck(t *testing.T, luaScript, message string, action int, score float64) {
	config := GetNewConfig()
	config.LuaModule = make(map[string]*ConfigLuaModule)
	config.LuaModule["test"] = &ConfigLuaModule{
		Enabled:        true,
		ScriptContents: luaScript,
	}

	SetConfig(config)
	LuaStart()

	done := make(chan bool)
	res := LuaMilterCheck("test", &Message{}, done)
	LuaReset()
	DaemonReset()

	if res.module != "lua-test" {
		t.Fatal("Expected module name 'lua-test', but got:", res.module)
	}

	if res.suggestedAction != action {
		t.Fatalf("Expected suggested action '%d', but got: %d", action, res.suggestedAction)
	}

	if res.message != message {
		t.Fatalf("Expected message '%s', but got: %s", message, res.message)
	}

	if res.score != score {
		t.Fatalf("Expected score '%f', but got: %f", score, res.score)
	}
}
