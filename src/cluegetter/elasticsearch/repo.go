// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package elasticsearch

import (
	"encoding/json"

	"cluegetter/address"

	"gopkg.in/olivere/elastic.v3"
)

func (m *module) getSessionsByAddress(instances []string, address *address.Address) ([]*session, error) {
	addressQuery := func(prefix string) elastic.Query {
		if address.Local() == "" {
			return elastic.NewMatchQuery(prefix+".Domain", address.Domain())
		}

		return elastic.NewBoolQuery().Must(
			elastic.NewMatchQuery(prefix+".Local", address.Local()),
			elastic.NewMatchQuery(prefix+".Domain", address.Domain()),
		)
	}

	query := elastic.NewBoolQuery().Must(
		elastic.NewTermsQuery("InstanceId", stringSliceToIface(instances)...),
		elastic.NewNestedQuery("Messages",
			elastic.NewBoolQuery().Should(
				addressQuery("Messages.From"),
				elastic.NewNestedQuery("Messages.Rcpt",
					addressQuery("Messages.Rcpt"),
				),
			),
		),
	)

	sr, err := m.esClient.Search().
		Index("cluegetter-sessions").
		Query(query).
		Sort("DateConnect", false).
		From(0).Size(250).
		//Pretty(true).
		Do()
	if err != nil {
		return nil, err
	}

	sessions := make([]*session, 0)
	if sr == nil || sr.TotalHits() == 0 {
		return sessions, nil
	}

	for _, hit := range sr.Hits.Hits {
		session := &session{}
		if err := json.Unmarshal(*hit.Source, session); err != nil {
			return nil, err
		}
		for _, msg := range session.Messages {
			msg.SetSession(session.MilterSession)
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

func stringSliceToIface(in []string) []interface{} {
	out := make([]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}
