package moduleDemo

import (
	"cluegetter/address"
	"cluegetter/core"
)

const ModuleName = "demo"

type testModule struct {
	*core.BaseModule

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

func (m *testModule) SetCluegetter(cg *core.Cluegetter) {
	m.cg = cg
}

func (m *testModule) Init() {
	m.cg.Log.Notice("Initializing module demo")
}

func (m *testModule) Stop() {
	m.cg.Log.Notice("Stopping module demo")
}

func (m *testModule) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	m.cg.Log.Notice("Milter Checking Message %s", msg.QueueId)

	return nil
}

func (m *testModule) RecipientCheck(rcpt *address.Address) (verdict int, msg string) {
	m.cg.Log.Debug("Considering if we should accept recipient %s", rcpt.String())

	return core.MessagePermit, ""
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
