// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

type rspamdResponseCheckResult struct {
	Description string
	Name        string
	Score       float64
	Options     []string
}

type rspamdResponse struct {
	Default struct {
		IsSpam        bool
		IsSkipped     bool
		Score         float64
		RequiredScore float64
		Action        string
		CheckResults  []*rspamdResponseCheckResult
	}
	Urls      []string
	Emails    []string
	MessageId string
}

func init() {
	init := rspamdStart
	milterCheck := rspamdGetResult

	Register(&module{
		name:        "rspamd",
		init:        &init,
		milterCheck: &milterCheck,
	})
}

func rspamdStart() {
	if Config.Rspamd.Enabled != true {
		Log.Info("Skipping Rspamd module because it was not enabled in the config")
		return
	}

	Log.Info("Rspamd module started successfully")
}

func rspamdGetResult(msg *Message, abort chan bool) *MessageCheckResult {
	if !Config.Rspamd.Enabled {
		return nil
	}

	rawResult := rspamdGetRawResult(msg)
	parsedResponse := rspamdParseRawResult(rawResult)

	score := parsedResponse.Default.Score * Config.Rspamd.Multiplier

	return &MessageCheckResult{
		module:          "rspamd",
		suggestedAction: messageReject,
		message: "Our system has detected that this message is likely unsolicited mail (SPAM). " +
			"To reduce the amount of spam, this message has been blocked.",
		score: score,
		determinants: map[string]interface{}{
			"response":   parsedResponse,
			"multiplier": Config.Rspamd.Multiplier,
		},
	}
}

func rspamdParseRawResult(rawResult interface{}) *rspamdResponse {
	raw := rawResult.(map[string]interface{})
	res := &rspamdResponse{
		Urls:   make([]string, 0),
		Emails: make([]string, 0),
	}

	for k, v := range raw {
		switch k {
		case "default":
			defaults := v.(map[string]interface{})
			for kk, vv := range defaults {
				switch kk {
				case "is_spam":
					res.Default.IsSpam = vv.(bool)
				case "is_skipped":
					res.Default.IsSkipped = vv.(bool)
				case "score":
					res.Default.Score = vv.(float64)
				case "required_score":
					res.Default.RequiredScore = vv.(float64)
				case "action":
					res.Default.Action = vv.(string)
				default:
					if strings.ToUpper(kk) != kk {
						Log.Notice("Received unknown key in 'default' from Rspamd: ", kk)
						continue
					}

					checkResult := &rspamdResponseCheckResult{}
					for kkk, vvv := range vv.(map[string]interface{}) {
						switch kkk {
						case "description":
							checkResult.Description = vvv.(string)
						case "name":
							checkResult.Name = vvv.(string)
						case "score":
							checkResult.Score = vvv.(float64)
						case "options":
							for _, option := range vvv.([]interface{}) {
								checkResult.Options = append(checkResult.Options, option.(string))
							}
						}
					}
					res.Default.CheckResults = append(res.Default.CheckResults, checkResult)
				}
			}
		case "urls":
			for _, url := range v.([]interface{}) {
				res.Urls = append(res.Urls, url.(string))
			}
		case "emails":
			for _, email := range v.([]interface{}) {
				res.Emails = append(res.Emails, email.(string))
			}
		case "message-id":
			res.MessageId = v.(string)
		}
	}

	return res

}

func rspamdGetRawResult(msg *Message) interface{} {
	sess := *msg.session
	var reqBody = msg.String()

	url := fmt.Sprintf("http://%s:%d/check", Config.Rspamd.Host, Config.Rspamd.Port)
	client := &http.Client{}
	req, _ := http.NewRequest("GET", url, bytes.NewBuffer(reqBody))
	for _, rcpt := range msg.Rcpt {
		req.Header.Add("Rcpt", rcpt)
	}
	req.Header.Set("IP", sess.getIp())
	req.Header.Set("Helo", sess.getHelo())
	req.Header.Set("From", msg.From)
	req.Header.Set("Queue-Id", msg.QueueId)
	req.Header.Set("User", sess.getSaslUsername())
	res, err := client.Do(req)

	if err != nil {
		fmt.Println(err)
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)

	var parsed interface{}
	err = json.Unmarshal([]byte(string(body)), &parsed)
	if err != nil {
		fmt.Println("error:", err)
	}

	return parsed
}
