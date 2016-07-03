// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package rspamd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"cluegetter/core"
)

const ModuleName = "rspamd"

type module struct {
	*core.BaseModule

	cg *core.Cluegetter
}

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
	core.ModuleRegister(&module{})
}

func (m *module) Name() string {
	return ModuleName
}

func (m *module) SetCluegetter(cg *core.Cluegetter) {
	m.cg = cg
}

func (m *module) Enable() bool {
	return m.cg.Config.Rspamd.Enabled
}

func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	if !msg.Session().Config().Rspamd.Enabled {
		return nil
	}

	rawResult, err := m.getRawResult(msg)
	if err != nil {
		return &core.MessageCheckResult{
			Module:          ModuleName,
			SuggestedAction: core.MessageError,
			Score:           25,
			Determinants: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}
	parsedResponse := m.parseRawResult(rawResult)

	score := parsedResponse.Default.Score * msg.Session().Config().Rspamd.Multiplier

	return &core.MessageCheckResult{
		Module:          ModuleName,
		SuggestedAction: core.MessageReject,
		Message: "Our system has detected that this message is likely unsolicited mail (SPAM). " +
			"To reduce the amount of spam, this message has been blocked.",
		Score: score,
		Determinants: map[string]interface{}{
			"response":   parsedResponse,
			"multiplier": m.cg.Config.Rspamd.Multiplier,
		},
	}
}

func (m *module) parseRawResult(rawResult interface{}) *rspamdResponse {
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
						m.cg.Log.Noticef("Received unknown key in 'default' from Rspamd: ", kk)
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

func (m *module) getRawResult(msg *core.Message) (interface{}, error) {
	sess := *msg.Session()
	var reqBody = msg.String()

	url := fmt.Sprintf("http://%s:%d/check", m.cg.Config.Rspamd.Host, m.cg.Config.Rspamd.Port)
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	for _, rcpt := range msg.Rcpt {
		req.Header.Add("Rcpt", rcpt.String())
	}
	req.Header.Set("IP", sess.Ip)
	req.Header.Set("Helo", sess.Helo)
	req.Header.Set("From", msg.From.String())
	req.Header.Set("Queue-Id", msg.QueueId)
	req.Header.Set("User", sess.SaslUsername)
	res, err := client.Do(req)

	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)

	var parsed interface{}
	err = json.Unmarshal([]byte(string(body)), &parsed)
	if err != nil {
		return nil, err
	}

	return parsed, nil
}
