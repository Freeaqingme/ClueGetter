// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package elasticsearch

import (
	"context"
	"time"

	"cluegetter/address"
	"cluegetter/core"

	"gopkg.in/olivere/elastic.v5"
)

type Finder struct {
	module *Module
	limit  int

	queueId       string
	from          *address.Address
	fromSld       string
	to            *address.Address
	saslUser      string
	clientAddress string
	dateStart     *time.Time
	dateEnd       *time.Time
	instances     []string
	verdicts      []int
	as            string
	ipRange       string
	country       string
}

type FinderResponse struct {
	Total    int64
	Sessions []core.SingleJsonableMsgSession

	DateHistogram24Hrs  map[int64]int64
	DateHistogram30Days map[int64]int64
	DateHistogram1Yrs   map[int64]int64
}

func (m *Module) NewFinder() *Finder {
	return &Finder{
		module: m,
		limit:  250,

		from: &address.Address{},
		to:   &address.Address{},

		verdicts: []int{0, 1, 2, 3},
	}
}

func (f *Finder) Limit() int {
	return f.limit
}

func (f *Finder) From() *address.Address {
	return f.from
}

func (f *Finder) FromSld() string {
	return f.fromSld
}

func (f *Finder) To() *address.Address {
	return f.to
}

func (f *Finder) SaslUser() string {
	return f.saslUser
}

func (f *Finder) ClientAddress() string {
	return f.clientAddress
}

func (f *Finder) QueueId() string {
	return f.queueId
}

func (f *Finder) DateStart() *time.Time {
	return f.dateStart
}

func (f *Finder) DateEnd() *time.Time {
	return f.dateEnd
}

func (f *Finder) Instances() []string {
	return f.instances
}

func (f *Finder) Verdicts() []int {
	return f.verdicts
}

func (f *Finder) AS() string {
	return f.as
}

func (f *Finder) IpRange() string {
	return f.ipRange
}

func (f *Finder) Country() string {
	return f.country
}

func (f *Finder) SetLimit(limit int) *Finder {
	f.limit = limit
	return f
}

func (f *Finder) SetFrom(from *address.Address) *Finder {
	f.from = from
	return f
}

func (f *Finder) SetFromSld(sld string) *Finder {
	f.fromSld = sld
	return f
}

func (f *Finder) SetTo(to *address.Address) *Finder {
	f.to = to
	return f
}

func (f *Finder) SetSaslUser(user string) *Finder {
	f.saslUser = user
	return f
}

func (f *Finder) SetClientAddress(ip string) *Finder {
	f.clientAddress = ip
	return f
}

func (f *Finder) SetDateStart(start *time.Time) *Finder {
	f.dateStart = start
	return f
}

func (f *Finder) SetDateEnd(end *time.Time) *Finder {
	f.dateEnd = end
	return f
}

func (f *Finder) SetInstances(instances []string) *Finder {
	f.instances = instances
	return f
}

func (f *Finder) SetVerdicts(verdicts []int) *Finder {
	f.verdicts = verdicts
	return f
}

func (f *Finder) SetQueueId(id string) *Finder {
	f.queueId = id
	return f
}

func (f *Finder) SetAS(as string) *Finder {
	f.as = as
	return f
}

func (f *Finder) SetIpRange(ipRange string) *Finder {
	f.ipRange = ipRange
	return f
}

func (f *Finder) SetCountry(country string) *Finder {
	f.country = country
	return f
}

func (f *Finder) find(resp *FinderResponse, supplementSearch func(*elastic.SearchService)) (*elastic.SearchResult, error) {
	search := f.module.esClient.Search().
		Index("cluegetter-sessions").
		Sort("DateConnect", false).
		From(0).
		Size(f.Limit())
	f.query(search)
	supplementSearch(search)

	sr, err := search.Do(context.TODO())
	if err != nil {
		return sr, err
	}

	resp.Total = sr.Hits.TotalHits
	resp.Sessions, err = f.decodeSessions(sr)
	if err != nil {
		return sr, err
	}

	return sr, nil
}

func (f *Finder) FindWithDateHistogram() (*FinderResponse, error) {
	resp := &FinderResponse{
		DateHistogram24Hrs:  make(map[int64]int64, 0),
		DateHistogram30Days: make(map[int64]int64, 0),
		DateHistogram1Yrs:   make(map[int64]int64, 0),
	}

	sr, err := f.find(resp, func(search *elastic.SearchService) {
		f.aggs(search)
	})

	if err != nil {
		return resp, err
	}

	parseAggregation := func(name string, store map[int64]int64) {
		aggParent, _ := sr.Aggregations.Nested(name)
		agg, _ := aggParent.DateHistogram("sessions")
		for _, bucket := range agg.Buckets {
			store[int64(bucket.Key)] = bucket.DocCount
		}
	}

	parseAggregation("DateHistogram24Hrs", resp.DateHistogram24Hrs)
	parseAggregation("DateHistogram30Days", resp.DateHistogram30Days)
	parseAggregation("DateHistogram1Yrs", resp.DateHistogram1Yrs)

	return resp, nil
}

func (f *Finder) aggs(service *elastic.SearchService) *elastic.SearchService {

	addAgg := func(name, interval, period string) {
		dateAgg := elastic.NewDateHistogramAggregation().
			Field("DateConnect").
			Interval(interval).
			Format("yyyy-MM-dd HH:mm").
			ExtendedBoundsMax("now").
			TimeZone("CET") // Do more intelligently?
		filter := elastic.NewRangeQuery("DateConnect").
			Gt(period)
		agg := elastic.NewFilterAggregation().Filter(filter).
			SubAggregation("sessions", dateAgg)
		service = service.Aggregation(name, agg)
	}

	addAgg("DateHistogram24Hrs", "15m", "now-24h")
	addAgg("DateHistogram30Days", "2h", "now-30d")
	addAgg("DateHistogram1Yrs", "1d", "now-365d")

	return service
}

func (f *Finder) query(service *elastic.SearchService) *elastic.SearchService {
	q := elastic.NewBoolQuery()
	if len(f.instances) > 0 && len(f.instances) != len(core.HttpGetInstances()) {
		q.Must(elastic.NewTermsQuery("InstanceId", stringSliceToIface(f.instances)...))
	}

	if f.from.String() != "" {
		q.Must(addressQuery("Messages.From", f.from))
	}
	if f.FromSld() != "" {
		q.Must(elastic.NewMatchQuery("Messages.From.Sld", f.FromSld()))
	}
	if f.to.String() != "" {
		q.Must(elastic.NewNestedQuery("Messages.Rcpt",
			addressQuery("Messages.Rcpt", f.to),
		))
	}
	if f.queueId != "" {
		q.Must(elastic.NewMatchQuery("Messages.QueueId", f.queueId))
	}
	if len(f.verdicts) != 0 && len(f.verdicts) != 4 {
		qVerdict := elastic.NewBoolQuery()
		for _, verdict := range f.verdicts {
			qVerdict.Should(elastic.NewTermQuery("Messages.Verdict", verdict))
		}
		q.Must(qVerdict)
	}

	if f.saslUser != "" {
		q.Must(elastic.NewTermsQuery("SaslUsername", f.saslUser))
	}
	if f.clientAddress != "" {
		q.Must(elastic.NewTermsQuery("Ip", f.clientAddress))
	}
	if f.AS() != "" {
		q.Must(elastic.NewTermsQuery("IpInfo.ASN", f.AS()))
	}
	if f.IpRange() != "" {
		q.Must(elastic.NewTermsQuery("IpInfo.IpRange", f.IpRange()))
	}
	if f.Country() != "" {
		q.Must(elastic.NewTermsQuery("IpInfo.Country", f.Country()))
	}

	return service.Query(q)
}

// For the time being we return SingleJsonableMsgSession, instead of regular Session
// because the determinants are not unmarshallable, for the time being :(
func (f *Finder) decodeSessions(sr *elastic.SearchResult) ([]core.SingleJsonableMsgSession, error) {
	sessions := make([]core.SingleJsonableMsgSession, 0)
	if sr == nil || sr.TotalHits() == 0 {
		return sessions, nil
	}

	for _, hit := range sr.Hits.Hits {
		session := &core.SingleJsonableMsgSession{}
		if err := session.UnmarshalJSON(*hit.Source); err != nil {
			return nil, err
		}
		for _, msg := range session.Messages {
			msg.SetSession(session.MilterSession)
		}
		sessions = append(sessions, *session)
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

func addressQuery(prefix string, address *address.Address) elastic.Query {
	if address.Local() == "" {
		return elastic.NewMatchQuery(prefix+".Domain", address.Domain())
	}

	return elastic.NewBoolQuery().Must(
		elastic.NewMatchQuery(prefix+".Local", address.Local()),
		elastic.NewMatchQuery(prefix+".Domain", address.Domain()),
	)
}
