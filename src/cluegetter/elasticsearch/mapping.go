package elasticsearch

// Needs updating with every significant mapping update
const mappingVersion = "2"

var mappingTemplate = `{
  "template": "cluegetter-*-%%MAPPING_VERSION%%",
  "settings": {
    "number_of_shards": 5
  },
  "aliases" : {
    "cluegetter-sessions" : {}
  },
  "settings":{
    "analysis": {
      "analyzer": {
        "lowercase": {
          "type": "custom",
          "tokenizer": "keyword",
          "filter": [
            "lowercase"
          ]
        }
      }
    }
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
        "SaslUsername":   {
          "type":     "string",
          "analyzer": "lowercase"
        },
        "SaslSender":     {
          "type":     "string",
          "analyzer": "lowercase"
        },
        "SaslMethod":     {
          "type":  "string",
          "index": "not_analyzed"
        },
        "CertIssuer":     { "type": "string"  },
        "CipherBits":     { "type": "short"   },
        "Cipher":         { "type": "string"  },
        "TlsVersion":     {
          "type":  "string",
          "index": "not_analyzed"
        },
        "Ip":             {
          "type":     "string",
          "analyzer": "lowercase"
        },
        "IpInfo": {
          "properties": {
            "ASN": {
              "type":     "string",
              "analyzer": "lowercase"
            },
            "ISP": {
              "type":     "string",
              "analyzer": "lowercase"
            },
            "IpRange": {
              "type":     "string",
              "analyzer": "lowercase"
            },
            "AllocationDate": { "type": "date" },
            "Country": {
              "type":     "string",
              "analyzer": "lowercase"
            },
            "Continent": {
              "type":     "string",
              "analyzer": "lowercase"
            },
            "location": { "type": "geo_point" }
          }
        },
        "ReverseDns":     { "type": "string"  },
        "Hostname":       { "type": "string"  },
        "Helo":           { "type": "string"  },
        "MtaHostName":    { "type": "string"  },
        "MtaDaemonName":  { "type": "string"  },

        "Messages": {
          "properties": {
            "QueueId": {
              "type":  "string",
              "index": "not_analyzed"
            },
            "From": {
              "properties": {
                "Local": {
                  "type":     "string",
                  "analyzer": "lowercase"
                },
                "Domain": {
                  "type":     "string",
                  "analyzer": "lowercase"
                },
                "Sld": {
                  "type":     "string",
                  "analyzer": "lowercase"
                }
              }
            },
            "Rcpt": {
              "type": "nested",
              "properties": {
                "Local":  {
                  "type":     "string",
                  "analyzer": "lowercase"
                },
                "Domain": {
                  "type":     "string",
                  "analyzer": "lowercase"
                },
                "Sld": {
                  "type":     "string",
                  "analyzer": "lowercase"
                }

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
            "VerdictMsg":             {
              "type":     "string",
              "analyzer": "simple"
            },
            "RejectScore":            { "type": "float"   },
            "RejectScoreThreshold":   { "type": "float"   },
            "TempfailScore":          { "type": "float"   },
            "TempfailScoreThreshold": { "type": "float"   },

            "CheckResults": {
              "type": "nested",
              "properties": {
                "Module":         {
                  "type":  "string",
                  "index": "not_analyzed"
                },
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
