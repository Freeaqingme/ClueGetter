package lua

import (
	"testing"

	"cluegetter/core"
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

module.sessionConfigure = function(session)
    session:config("Greylisting.Enabled", true)
end

return module
	`

	testLuaMilterCheck(t, luaScript, "", 3, -25.4)
}

func TestSessionConfigure(t *testing.T) {
	luaScript := `
local module = {}

module.milterCheck = function(message)
    return "ERROR", "", -25.4
end

module.sessionConfigure = function(session)
    session:config("Greylisting.Enabled", true)
    session:config("DkIm.sIgn", "foobar")
end

return module
	`

	core.DaemonReset()

	module, config := getTestModule()

	config["test"] = &core.ConfigLuaModule{
		Enabled:        true,
		ScriptContents: luaScript,
	}

	module.Init()

	cg := core.NewCluegetter()
	sess := cg.NewMilterSession()
	module.modules["test"].SessionConfigure(sess)

	if !sess.Config().Greylisting.Enabled {
		t.Fatal("Greylisting should have been Enabled, but wasn't")
	}

	if sess.Config().Dkim.Sign != "foobar" {
		t.Fatalf("Expected Dkim.Sign to be 'foobar', but got: %s", sess.Config().Dkim.Sign)
	}
}

func testLuaMilterCheck(t *testing.T, luaScript, message string, action int, score float64) {
	core.DaemonReset()

	module, config := getTestModule()

	config["test"] = &core.ConfigLuaModule{
		Enabled:        true,
		ScriptContents: luaScript,
	}

	module.Init()
	done := make(chan bool)
	res := module.modules["test"].MessageCheck(&core.Message{}, done)

	if res == nil {
		t.Fatal("Expected an instance of MessageCheckResult, but got <nil>")
	}

	if res.Module != "lua-test" {
		t.Fatal("Expected module name 'lua-test', but got:", res.Module)
	}

	if res.SuggestedAction != action {
		t.Fatalf("Expected suggested action '%d', but got: %d", action, res.SuggestedAction)
	}

	if res.Message != message {
		t.Fatalf("Expected message '%s', but got: %s", message, res.Message)
	}

	if res.Score != score {
		t.Fatalf("Expected score '%f', but got: %f", score, res.Score)
	}
}

func getTestModule() (*module, map[string]*core.ConfigLuaModule) {
	baseMod, config := core.NewBaseModuleForTesting(nil)

	module := &module{
		BaseModule: baseMod,
	}

	return module, config.LuaModule
}
