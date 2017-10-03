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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"cluegetter/core"

	"github.com/Freeaqingme/dmarcaggparser/dmarc"
	"gopkg.in/olivere/elastic.v5"
)

const ModuleName = "elasticsearch"

type Module struct {
	*core.BaseModule

	esClient *elastic.Client
}

func init() {
	core.ModuleRegister(&Module{
		BaseModule: core.NewBaseModule(nil),
	})
}

func (m *Module) Name() string {
	return ModuleName
}

func (m *Module) Enable() bool {
	return m.Config().Elasticsearch.Enabled
}

func (m *Module) Init() error {
	var err error
	m.esClient, err = elastic.NewClient(
		elastic.SetSniff(m.Config().Elasticsearch.Sniff),
		elastic.SetURL(m.Config().Elasticsearch.Url...),
	)
	if err != nil {
		return fmt.Errorf("Could not connect to ElasticSearch: %s", err.Error())
	}

	template := strings.Replace(mappingTemplate, "%%MAPPING_VERSION%%", mappingVersion, -1)

	_, err = m.esClient.IndexPutTemplate("cluegetter-session" + mappingVersion).
		BodyString(template).
		Do(context.TODO())
	if err != nil {
		return fmt.Errorf("Could not create ES session template: %s", err.Error())
	}

	if reportsModule := m.Module("reports", ""); reportsModule != nil {
		template = strings.Replace(mappingTemplateDmarcReport, "%%MAPPING_VERSION%%", mappingVersionDmarcReport, -1)

		_, err = m.esClient.IndexPutTemplate("cluegetter-session" + mappingVersionDmarcReport).
			BodyString(template).
			Do(context.TODO())
		if err != nil {
			return fmt.Errorf("Could not create ES dmarc report template: %s", err.Error())
		}
	}

	return nil
}

func (m *Module) SessionDisconnect(sess *core.MilterSession) {
	m.persistSession(sess)
}

// TODO: Check what happens if we added a message-id header ourselves
//
// Because aggregations don't work too nicely on nested documents we
// denormalize our sessions, so we store 1 session per message.
// That way we don't need nested documents for messages.
func (m *Module) persistSession(sess *core.MilterSession) {
	if sess.ClientIsMonitorHost() && len(sess.Messages) == 0 {
		return
	}

	for msgId, msg := range sess.Messages {
		str, _ := sess.MarshalJSONWithSingleMessage(msg, false)
		sessId := fmt.Sprintf("%s-%d", hex.EncodeToString(sess.Id()), msgId)

		_, err := m.esClient.Index().
			Index(fmt.Sprintf("cluegetter-session-%s-%s",
				sess.DateConnect.Format("20060102"),
				mappingVersion)).
			Type("session").
			Id(sessId).
			BodyString(string(str)).
			Do(context.TODO())

		if err != nil {
			m.Log().Errorf("Could not index session '%s', error: %s", sessId, err.Error())
		}
	}

	//fmt.Printf("Indexed tweet %s to index %s, type %s\n", put1.Id, put1.Index, put1.Type)
}

func (m *Module) DmarcReportPersist(report *dmarc.FeedbackReport) {
	str, _ := json.Marshal(report)

	_, err := m.esClient.Index().
		Index(fmt.Sprintf("cluegetter-dmarcreport-%s-%s",
			report.Metadata.DateRange.Begin.Format("20060102"),
			mappingVersionDmarcReport)).
		Type("dmarcReport").
		Id(report.Metadata.ReportId + "@" + report.Metadata.OrgName).
		BodyString(string(str)).
		Do(context.TODO())

	if err != nil {
		m.Log().Errorf("Could not index DMARC Report, error: %s", err.Error())
	}
}
