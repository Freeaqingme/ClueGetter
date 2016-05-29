package main

import (
	"encoding/hex"
	"encoding/json"
	"time"

	"gopkg.in/olivere/elastic.v3"
)

var esClient *elastic.Client

func init() {
	enable := func() bool { return Config.Elasticsearch.Enabled }
	init := esStart

	ModuleRegister(&module{
		name:   "elasticsearch",
		enable: &enable,
		init:   &init,
	})
}

func esStart() {
	var err error
	esClient, err = elastic.NewClient(elastic.SetURL(Config.Elasticsearch.Url...))
	if err != nil {
		Log.Fatal("Could not connect to ElasticSearch: %s", err.Error())
	}

	template := `{
  "template": "cluegetter-*",
  "settings": {
    "number_of_shards": 5
  },
  "aliases" : {
    "cluegetter-messages" : {}
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
		Log.Fatal("Could not create ES template: %s", err.Error())
	}
}

func esSaveSession(sess *milterSession) {
	if !Config.Elasticsearch.Enabled {
		return
	}

	str, _ := sess.esMarshalJSON()
	id := hex.EncodeToString(sess.id[:])

	_, err := esClient.Index().
		Index("cluegetter-" + time.Now().Format("20060102")).
		Type("session").
		Id(id).
		BodyString(string(str)).
		Do()

	if err != nil {
		Log.Error("Could not index email %s, error: %s", id, err.Error())
		return
	}
	//fmt.Printf("Indexed tweet %s to index %s, type %s\n", put1.Id, put1.Index, put1.Type)
}

func (s *milterSession) esMarshalJSON() ([]byte, error) {
	type Alias milterSession

	esMessages := []*esMessage{}
	for _, v := range s.Messages {
		esMessages = append(esMessages, &esMessage{v})
	}

	out := &struct {
		InstanceId uint
		*Alias
		EsMessages []*esMessage `json:"Messages"`
	}{
		InstanceId: instance,
		Alias:      (*Alias)(s),
		EsMessages: esMessages,
	}

	return json.Marshal(out)
}

type esMessage struct {
	*Message
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
