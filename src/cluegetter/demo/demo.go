package moduleDemo

import (
	"cluegetter/core"
)

const ModuleName = "demo"

type testModule struct {
	cg *core.Cluegetter
}

func init() {
	core.ModuleRegister(&testModule{})
}

func (m *testModule) Name() string {
	return ModuleName
}

func (m *testModule) Enable() bool {
	return true
}

func (m *testModule) Init(cg *core.Cluegetter) {
	m.cg = cg

	m.cg.Log.Notice("Initializing module test")
}

func (m *testModule) Stop() {
	m.cg.Log.Notice("Stopping module test")
}

func (m *testModule) MilterCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	m.cg.Log.Notice("Milter Checking Message %s", msg.QueueId)

	return nil
}

func (m *testModule) Ipc() map[string]func(string) {
	return make(map[string]func(string), 0)
}

func (m *testModule) Rpc() map[string]chan string {
	return make(map[string]chan string, 0)
}

func (m *testModule) HttpHandlers() map[string]core.HttpCallback {
	return make(map[string]core.HttpCallback, 0)
}
