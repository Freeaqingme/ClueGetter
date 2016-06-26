// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package elasticsearch

import (
	"fmt"
	"net/http"

	"cluegetter/address"
	"cluegetter/core"

	"gopkg.in/olivere/elastic.v3"
)

func (m *module) HttpHandlers() map[string]core.HttpCallback {
	return map[string]core.HttpCallback{
		"/es/message/searchEmail/": func(w http.ResponseWriter, r *http.Request) {
			m.httpHandlerMessageSearchEmail(w, r)
		},
	}
}

func (m *module) httpHandlerMessageSearchEmail(w http.ResponseWriter, r *http.Request) {
	address := address.FromAddressOrDomain(r.URL.Path[len("/es/message/searchEmail/"):])

	instances, err := core.HttpParseFilterInstance(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	query := elastic.NewBoolQuery().Must(
		elastic.NewTermsQuery("InstanceId", stringSliceToIface(instances)...),
		elastic.NewNestedQuery("Messages",
			elastic.NewBoolQuery().Should(
				elastic.NewBoolQuery().Must(
					elastic.NewMatchQuery("Messages.From.Local", address.Local()),
					elastic.NewMatchQuery("Messages.From.Domain", address.Domain()),
				),
				elastic.NewNestedQuery("Messages.Rcpt",
					elastic.NewBoolQuery().Must(
						elastic.NewMatchQuery("Messages.Rcpt.Local", address.Local()),
						elastic.NewMatchQuery("Messages.Rcpt.Domain", address.Domain()),
					),
				),
			),
		),
	)

	searchResult, err := m.esClient.Search().
		Index("cluegetter-sessions").
		Query(query).
		Sort("DateConnect", false).
		From(0).Size(250).
		//Pretty(true).
		Do()
	if err != nil {
		// Handle error
		panic(err)
	}

	// searchResult is of type SearchResult and returns hits, suggestions,
	// and all kinds of other information from Elasticsearch.
	fmt.Printf("Query took %d milliseconds\n", searchResult.TookInMillis)

	for k, v := range searchResult.Hits.Hits {
		fmt.Println(k, v)
	}
}

func stringSliceToIface(in []string) []interface{} {
	out := make([]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}
