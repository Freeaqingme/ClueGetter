// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"fmt"
	"gopkg.in/redis.v3"
)

func init() {
	enable := func() bool { return Config.Contacts.Enabled }
	milterCheck := contactsGetResult
	init := contactsInit

	ModuleRegister(&module{
		name:        "contacts",
		enable:      &enable,
		init:        &init,
		milterCheck: &milterCheck,
	})
}

func contactsInit() {
	if !Config.Redis.Enabled {
		Log.Fatal("The contacts module requires the Redis module to be enabled")
	}
}

func contactsGetResult(msg *Message, abort chan bool) *MessageCheckResult {
	conf := Config.Contacts
	if !conf.Enabled || !msg.session.config.Contacts.Enabled {
		return nil
	}

	if msg.From.Domain() == "" || msg.Rcpt[0].Domain() == "" {
		return &MessageCheckResult{
			module:          "contacts",
			suggestedAction: messagePermit,
			score:           0,
			determinants: map[string]interface{}{
				"address":   msg.From.String(),
				"domain":    msg.From.Domain(),
				"unchecked": "true",
				"reason":    "From Domain or First Recipient Domain were not present",
			},
		}
	}

	if msg.From.String() == msg.Rcpt[0].String() {
		return &MessageCheckResult{
			module:          "contacts",
			suggestedAction: messagePermit,
			score:           0,
			determinants: map[string]interface{}{
				"address":   msg.From.String(),
				"domain":    msg.From.Domain(),
				"unchecked": "true",
				"reason":    "From and First Recipient Address were equal",
			},
		}
	}

	tpl := fmt.Sprintf("cluegetter-%d-contacts-%%s-%%s-rcpt-domain-%s", instance, msg.Rcpt[0].Domain())
	ret := func(score float64, list, value string) *MessageCheckResult {
		return &MessageCheckResult{
			module:          "contacts",
			suggestedAction: messageReject,
			score:           score,
			message: "This message was sent from an address that was put on a local blacklist " +
				"by one of the recipients. Therefore, the message has been blocked.",
			determinants: map[string]interface{}{
				"list":  list,
				"value": value,
				"key":   tpl,
			},
		}
	}

	check := contactsAppearsOnList
	if check(fmt.Sprintf(tpl, "whitelist", "address"), msg.From.String()) {
		return ret(conf.Whitelist_Address_Score, "whitelist", msg.From.String())
	}
	if check(fmt.Sprintf(tpl, "blacklist", "address"), msg.From.String()) {
		return ret(conf.Blacklist_Address_Score, "blacklist", msg.From.String())
	}
	if check(fmt.Sprintf(tpl, "whitelist", "domain"), msg.From.Domain()) {
		return ret(conf.Whitelist_Domain_Score, "whitelist", msg.From.Domain())
	}
	if check(fmt.Sprintf(tpl, "blacklist", "domain"), msg.From.Domain()) {
		return ret(conf.Blacklist_Domain_Score, "blacklist", msg.From.Domain())
	}

	return &MessageCheckResult{
		module:          "contacts",
		suggestedAction: messagePermit,
		score:           0,
		determinants: map[string]interface{}{
			"key":     tpl,
			"address": msg.From.String(),
			"domain":  msg.From.Domain(),
		},
	}
}

func contactsAppearsOnList(list, entry string) bool {
	script := redis.NewScript(`
		local elements = redis.call('lrange', KEYS[1], 0, -1 )
		for i=1,#elements do
			if elements[i] == ARGV[1] then
				return 1
			end
		end

		return 0
	`)

	n, err := script.EvalSha(redisClient, []string{list}, []string{entry}).Result()
	if err != nil {
		Log.Error("Redis error in contactsAppearsOnList(): %s", err.Error())
		return false
	}

	return (n.(int64) != 0)
}
