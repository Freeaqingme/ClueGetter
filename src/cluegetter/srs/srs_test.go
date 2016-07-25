package srs

import (
	"testing"

	"cluegetter/address"
	"cluegetter/core"

	logging "github.com/Freeaqingme/GoDaemonSkeleton/log"
)

func TestIsForwardedWithoutOrigHeader(t *testing.T) {
	rcpt := make([]*address.Address, 0)
	msg := &core.Message{
		Rcpt: append(rcpt, address.FromString("srs@example.com")),
	}

	module := &srsModule{
		cg: &core.Cluegetter{},
	}

	if module.isForwarded(msg) {
		t.Fatal("Message without X-Orig-To header was said to be forwarded")
	}
}

func TestIsForwardedWithHeader(t *testing.T) {
	rcpt := make([]*address.Address, 0)

	getMsgWithOrigToValue := func(value string) *core.Message {
		msg := &core.Message{
			Rcpt: append(rcpt, address.FromString("srs@example.com")),
		}
		msg.Headers = append(msg.Headers, core.MessageHeader{Key: "foo", Value: "bar"})
		msg.Headers = append(msg.Headers, core.MessageHeader{Key: "X-ORIgiNAL-To", Value: value})
		return msg
	}

	values := []string{"srs2@example.net", "srs@example.net", "srs2@example.com"}
	for _, value := range values {
		msg := getMsgWithOrigToValue(value)

		if !testGetSrsModule().isForwarded(msg) {
			t.Fatal("Message with X-Orig-To header was said to not be forwarded")
		}
	}

	values = []string{"srs@example.com", "SRS@exAMPle.com"}
	for _, value := range values {
		msg := getMsgWithOrigToValue(value)

		if testGetSrsModule().isForwarded(msg) {
			t.Fatalf("Message with X-Orig-To hdr matching recipient was said to be forwarded: %s", value)
		}
	}
}

func TestGetRewriteDomain(t *testing.T) {
	rcpt := []*address.Address{
		address.FromString("srs@example.com"),
		address.FromString("srs@example.net"),
		address.FromString("srs@example.org"),
	}
	msg := &core.Message{
		Rcpt: rcpt,
	}
	msg.Headers = append(msg.Headers, core.MessageHeader{Key: "foo", Value: "bar"})
	msg.Headers = append(msg.Headers, core.MessageHeader{Key: "X-ORIgiNAL-To", Value: "srs@example.com"})
	msg.Headers = append(msg.Headers, core.MessageHeader{Key: "X-ORIgiNAL-To", Value: "srs@example.blaat"})
	msg.Headers = append(msg.Headers, core.MessageHeader{Key: "X-ORIgiNAL-To", Value: "srs@example.net"})

	if res := testGetSrsModule().getRewriteDomain(msg); res != "example.blaat" {
		t.Fatalf("Expected 'example.blaat' but got '%s'", res)
	}
}

func TestGetRewriteDomainNoOrigToHeaders(t *testing.T) {
	rcpt := []*address.Address{
		address.FromString("srs@example.com"),
		address.FromString("srs@example.net"),
		address.FromString("srs@example.org"),
	}
	msg := &core.Message{
		Rcpt: rcpt,
	}

	if res := testGetSrsModule().getRewriteDomain(msg); res != "" {
		t.Fatalf("Expected '' but got '%s'", res)
	}
}

func TestGetRewriteDomainMultipleMatches(t *testing.T) {
	rcpt := []*address.Address{
		address.FromString("srs@example.com"),
		address.FromString("srs@example.net"),
		address.FromString("srs@example.org"),
	}
	msg := &core.Message{
		QueueId: "1337",
		Rcpt:    rcpt,
	}
	msg.Headers = append(msg.Headers, core.MessageHeader{Key: "foo", Value: "bar"})
	msg.Headers = append(msg.Headers, core.MessageHeader{Key: "X-ORIgiNAL-To", Value: "srs@example.com"})
	msg.Headers = append(msg.Headers, core.MessageHeader{Key: "X-ORIgiNAL-To", Value: "srs@example.foobar"})
	msg.Headers = append(msg.Headers, core.MessageHeader{Key: "X-ORIgiNAL-To", Value: "srs@example.blaat"})

	if res := testGetSrsModule().getRewriteDomain(msg); res != "example.foobar" {
		t.Fatalf("Expected 'example.foobar' but got '%s'", res)
	}
}

func TestGetFromAddress(t *testing.T) {
	rcpt := []*address.Address{
		address.FromString("bob@example.com"),
	}
	msg := &core.Message{
		QueueId: "1337",
		From:    address.FromString("alice@example.net"),
		Rcpt:    rcpt,
	}
	msg.Headers = append(msg.Headers, core.MessageHeader{Key: "X-Original-To", Value: "carol@example.org"})

	if res := testGetSrsModule().getFromAddress(msg); res != "SRS0=1337=example.net=alice@example.org" {
		t.Fatalf("Expected 'SRS0=1337=example.net=alice@example.org' but got '%s'", res)
	}
}

func testGetSrsModule() *srsModule {
	module := &srsModule{
		cg: &core.Cluegetter{},
	}
	core.DefaultConfig(&module.cg.config)
	module.cg.config.Srs.Enabled = true

	module.cg.log = logging.Open("testing", "DEBUG")

	return module
}
