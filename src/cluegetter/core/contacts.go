// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"fmt"
	"gopkg.in/redis.v3"
	"strings"
)

func init() {
	enable := func() bool { return Config.Contacts.Enabled }
	milterCheck := contactsGetResult
	init := contactsInit

	ModuleRegister(&ModuleOld{
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
	if !msg.session.config.Contacts.Enabled {
		return nil
	}

	if msg.From.Domain() == "" || msg.Rcpt[0].Domain() == "" {
		return &MessageCheckResult{
			Module:          "contacts",
			SuggestedAction: MessagePermit,
			Score:           0,
			Determinants: map[string]interface{}{
				"address":   msg.From.String(),
				"domain":    msg.From.Domain(),
				"unchecked": "true",
				"reason":    "From Domain or First Recipient Domain were not present",
			},
		}
	}

	if msg.From.String() == msg.Rcpt[0].String() {
		return &MessageCheckResult{
			Module:          "contacts",
			SuggestedAction: MessagePermit,
			Score:           0,
			Determinants: map[string]interface{}{
				"address":   msg.From.String(),
				"domain":    msg.From.Domain(),
				"unchecked": "true",
				"reason":    "From and First Recipient Address were equal",
			},
		}
	}

	tpl := []string{
		fmt.Sprintf("cluegetter-%d-contacts-%%s-%%s-rcpt-domain-%s", instance, msg.Rcpt[0].Domain()),
		fmt.Sprintf("cluegetter-%d-contacts-%%s-%%s-rcpt-address-%s", instance, msg.Rcpt[0].String()),
	}

	ret := func(score float64, list, value string) *MessageCheckResult {
		return &MessageCheckResult{
			Module:          "contacts",
			SuggestedAction: MessageReject,
			Score:           score,
			Message: "This message was sent from an address that was put on a local blacklist " +
				"by one of the recipients. Therefore, the message has been blocked.",
			Determinants: map[string]interface{}{
				"list":  list,
				"value": value,
				"key":   tpl,
			},
		}
	}

	keys := func(tpls []string, list, scope string) []string {
		out := []string{}
		for _, tpl := range tpls {
			out = append(out, fmt.Sprintf(tpl, strings.ToLower(list), strings.ToLower(scope)))
		}
		return out
	}

	check := contactsAppearsOnList
	if check(keys(tpl, "whitelist", "address"), msg.From.String(), true) {
		return ret(conf.Whitelist_Address_Score, "whitelist", msg.From.String())
	}
	if check(keys(tpl, "blacklist", "address"), msg.From.String(), true) {
		return ret(conf.Blacklist_Address_Score, "blacklist", msg.From.String())
	}
	if check(keys(tpl, "whitelist", "domain"), msg.From.Domain(), true) {
		return ret(conf.Whitelist_Domain_Score, "whitelist", msg.From.Domain())
	}
	if check(keys(tpl, "blacklist", "domain"), msg.From.Domain(), true) {
		return ret(conf.Blacklist_Domain_Score, "blacklist", msg.From.Domain())
	}

	return &MessageCheckResult{
		Module:          "contacts",
		SuggestedAction: MessagePermit,
		Score:           0,
		Determinants: map[string]interface{}{
			"key":     tpl,
			"address": msg.From.String(),
			"domain":  msg.From.Domain(),
		},
	}
}

func contactsAppearsOnList(keys []string, entry string, retry bool) bool {
	scriptStr := `
		for x=1,#KEYS do
			local elements = redis.call('lrange', KEYS[x], 0, -1 )
			for i=1,#elements do
				if elements[i] == ARGV[1] then
					return 1
				end
			end
		end

		return 0
	`
	script := redis.NewScript(scriptStr)

	n, err := script.EvalSha(redisClient, keys, []string{strings.ToLower(entry)}).Result()
	if err != nil {
		if retry {
			Log.Notice("Redis error in contactsAppearsOnList(). Retrying..: %s", err.Error())
			script.Load(redisClient)
			return contactsAppearsOnList(keys, entry, false)
		}
		Log.Error("Redis error in contactsAppearsOnList(): %s", err.Error())
		return false
	}

	return (n.(int64) != 0)
}
