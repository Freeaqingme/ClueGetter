// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"encoding/json"
	"time"

	"cluegetter/address"
)

type SingleJsonableMsgSession struct {
	*MilterSession
}

type SingleJsonableMsg struct {
	*Message

	Body []byte
}

func (s *MilterSession) MarshalJSONWithSingleMessage(msg *Message, includeBody bool) ([]byte, error) {
	sess := &SingleJsonableMsgSession{s}
	return sess.MarshalJSON(msg, includeBody)
}

func (s *SingleJsonableMsgSession) MarshalJSON(msg *Message, includeBody bool) ([]byte, error) {
	type Alias SingleJsonableMsgSession

	var esMessages []*SingleJsonableMsg
	if includeBody {
		esMessages = []*SingleJsonableMsg{&SingleJsonableMsg{msg, msg.Body}}
	} else {
		esMessages = []*SingleJsonableMsg{&SingleJsonableMsg{msg, []byte{}}}
	}

	out := &struct {
		InstanceId uint
		*Alias
		EsMessages []*SingleJsonableMsg `json:"Messages"`
	}{
		InstanceId: s.Instance,
		Alias:      (*Alias)(s),
		EsMessages: esMessages,
	}

	return json.Marshal(out)
}

func (m *SingleJsonableMsg) MarshalJSON() ([]byte, error) {
	type Alias SingleJsonableMsg

	out := &struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	}

	return json.Marshal(out)
}

func (s *SingleJsonableMsgSession) UnmarshalJSON(data []byte) error {
	type Alias SingleJsonableMsgSession

	aux := &struct {
		*Alias
		InstanceId uint
		Messages   []SingleJsonableMsg
	}{
		Alias:    (*Alias)(s),
		Messages: make([]SingleJsonableMsg, 0),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	aux.Alias.Messages = make([]*Message, 0)
	for _, msg := range aux.Messages {
		aux.Alias.Messages = append(aux.Alias.Messages, (*Message)(msg.Message))
	}

	s.Instance = aux.InstanceId
	return nil
}

func (m *SingleJsonableMsg) UnmarshalJSON(data []byte) error {
	type Alias SingleJsonableMsg

	aux := &struct {
		*Alias
		From struct {
			Local  string
			Domain string
		}
		Rcpt []struct {
			Local  string
			Domain string
		}
		CheckResults []struct {
			Module          string
			SuggestedAction int `json:"Verdict"`
			Message         string
			Score           float64
			Determinants    string
			Duration        time.Duration
			WeightedScore   float64
		}
	}{
		Alias: (*Alias)(m),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	aux.Alias.From = address.FromString(aux.From.Local + "@" + aux.From.Domain)
	for _, v := range aux.Rcpt {
		aux.Alias.Rcpt = append(aux.Alias.Rcpt, address.FromString(v.Local+"@"+v.Domain))
	}
	for _, v := range aux.CheckResults {
		var determinants interface{}
		determinantsMap := make(map[string]interface{}, 0)
		var err error
		if err = json.Unmarshal([]byte(v.Determinants), &determinants); err != nil {
			determinantsMap["error"] = "Could not unmarshal determinants from Elasticsearch Database: " + err.Error()
		} else if determinants == nil {
			determinantsMap = make(map[string]interface{}, 0)
		} else {
			determinantsMap = determinants.(map[string]interface{})
		}

		aux.Alias.CheckResults = append(aux.Alias.CheckResults, &MessageCheckResult{
			Module:          v.Module,
			SuggestedAction: v.SuggestedAction,
			Score:           v.Score,
			Duration:        v.Duration,
			WeightedScore:   v.WeightedScore,
			Determinants:    determinantsMap,
		})
	}

	return nil
}
