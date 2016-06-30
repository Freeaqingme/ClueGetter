package address

import (
	"encoding/json"
	"strings"
)

type Address struct {
	local  string
	domain string
}

func (a *Address) Local() string {
	return a.local
}

func (a *Address) Domain() string {
	return a.domain
}

func (a *Address) String() string {
	if a.domain == "" {
		return a.local
	}

	return a.local + "@" + a.domain
}

func (a *Address) MarshalJSON() ([]byte, error) {
	type Alias Address
	return json.Marshal(&struct {
		Local   string
		Domain  string
		Address string
	}{
		a.Local(),
		a.Domain(),
		a.String(),
	})
}

func FromString(address string) *Address {
	a := &Address{}
	a.local, a.domain = messageParseAddress(address, true)

	return a
}

func FromAddressOrDomain(address string) *Address {
	a := &Address{}
	a.local, a.domain = messageParseAddress(address, false)

	return a
}

func messageParseAddress(address string, singleIsUser bool) (local, domain string) {
	if strings.Index(address, "@") != -1 {
		local = strings.SplitN(address, "@", 2)[0]
		domain = strings.SplitN(address, "@", 2)[1]
	} else if singleIsUser {
		local = address
		domain = ""
	} else {
		local = ""
		domain = address
	}

	return
}
