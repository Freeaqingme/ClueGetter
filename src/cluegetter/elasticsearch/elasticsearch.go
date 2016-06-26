// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package elasticsearch

import (
	"encoding/hex"
	"encoding/json"

	"cluegetter/core"

	"gopkg.in/olivere/elastic.v3"
)

const ModuleName = "elasticsearch"

var esClient *elastic.Client

type module struct {
	*core.BaseModule

	cg *core.Cluegetter
}

type session struct {
	*core.MilterSession
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
	return m.cg.Config.Elasticsearch.Enabled
}

func (m *module) Init() {
	var err error
	esClient, err = elastic.NewClient(
		elastic.SetSniff(m.cg.Config.Elasticsearch.Sniff),
		elastic.SetURL(m.cg.Config.Elasticsearch.Url...),
	)
	if err != nil {
		m.cg.Log.Fatal("Could not connect to ElasticSearch: ", err.Error())
	}

	template := `{
  "template": "cluegetter-*",
  "settings": {
    "number_of_shards": 5
  },
  "aliases" : {
    "cluegetter-sessions" : {}
  },
  "mappings": {
    "session": {
      "_all": {
        "enabled": false
      },
      "properties": {
        "InstanceId":     { "type": "integer" },
        "DateConnect":    { "type": "date"    },
        "DateDisconnect": { "type": "date"    },
        "SaslUsername":   { "type": "string"  },
        "SaslSender":     { "type": "string"  },
        "SaslMethod":     { "type": "string"  },
        "CertIssuer":     { "type": "string"  },
        "CipherBits":     { "type": "short"   },
        "Cipher":         { "type": "string"  },
        "TlsVersion":     { "type": "string"  },
        "Ip":             { "type": "string"  },
        "ReverseDns":     { "type": "string"  },
        "Hostname":       { "type": "string"  },
        "Helo":           { "type": "string"  },
        "MtaHostName":    { "type": "string"  },
        "MtaDaemonName":  { "type": "string"  },

        "Messages": {
          "type": "nested",
          "properties": {
            "QueueId": { "type": "string"  },
            "From": {
              "properties": {
                "Local":  { "type": "string" },
                "Domain": { "type": "string" }
              }
            },
            "Rcpt": {
              "type": "nested",
              "properties": {
                "Local":  { "type": "string" },
                "Domain": { "type": "string" }
              }
            },
            "Headers": {
              "type": "nested",
              "properties": {
                "Key":   { "type": "string" },
                "Value": { "type": "string" }
              }
            },

            "Date":                   { "type": "date"    },
            "BodySize":               { "type": "integer" },
            "BodyHash":               { "type": "string"  },
            "Verdict":                { "type": "integer" },
            "VerdictMsg":             { "type": "string"  },
            "RejectScore":            { "type": "float"   },
            "RejectScoreThreshold":   { "type": "float"   },
            "TempfailScore":          { "type": "float"   },
            "TempfailScoreThreshold": { "type": "float"   },

            "results": {
              "type": "nested",
              "properties": {
                "Module":         { "type": "string" },
                "Verdict":        { "type": "integer" },
                "Message":        { "type": "string" },
                "Score":          { "type": "float" },
                "WeightedScore":  { "type": "float" },
                "Duration":       { "type": "long" },
                "Determinants":   { "type": "string" }
              }
            }

          }
        }
      }
    }
  }
}
	`

	_, err = esClient.IndexPutTemplate("cluegetter").BodyString(template).Do()
	if err != nil {
		m.cg.Log.Fatal("Could not create ES template: %s", err.Error())
	}
}

func (m *module) SessionDisconnect(sess *core.MilterSession) {
	m.persistSession(sess)
}

func (m *module) persistSession(coreSess *core.MilterSession) {
	sess := &session{coreSess}

	str, _ := sess.esMarshalJSON(m)
	id := hex.EncodeToString(sess.Id())

	_, err := esClient.Index().
		Index("cluegetter-" + sess.DateConnect.Format("20060102")).
		Type("session").
		Id(id).
		BodyString(string(str)).
		Do()

	if err != nil {
		m.cg.Log.Error("Could not index session '%s', error: %s", id, err.Error())
		return
	}
	//fmt.Printf("Indexed tweet %s to index %s, type %s\n", put1.Id, put1.Index, put1.Type)

}

func (s *session) esMarshalJSON(m *module) ([]byte, error) {
	type Alias session

	esMessages := []*esMessage{}
	for _, v := range s.Messages {
		esMessages = append(esMessages, &esMessage{v})
	}

	out := &struct {
		InstanceId uint
		*Alias
		EsMessages []*esMessage `json:"Messages"`
	}{
		InstanceId: m.cg.Instance(),
		Alias:      (*Alias)(s),
		EsMessages: esMessages,
	}

	return json.Marshal(out)
}

type esMessage struct {
	*core.Message
}

func (m *esMessage) MarshalJSON() ([]byte, error) {
	type Alias esMessage

	out := &struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	}

	return json.Marshal(out)
}
