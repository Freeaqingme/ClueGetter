package main

import (
	"encoding/hex"
	"encoding/json"
	"time"

	"cluegetter/address"
	"flag"
	"fmt"
	"gopkg.in/cheggaaa/pb.v1"
	"gopkg.in/olivere/elastic.v3"
	"os"
	"strings"
	"sync"
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

	handover := esSubApp
	subAppRegister(&subApp{
		name:     "elasticsearch",
		handover: &handover,
	})
}

func esSubApp() {
	if len(os.Args) <= 1 || os.Args[1] != "index" {
		fmt.Println("SubApp 'elasticsearch' requires a second argument. Must be one of: index")
		os.Exit(1)
	}

	os.Args = os.Args[1:]
	notBefore := flag.Int64("notBefore", 0, "Unix Timestamp. Don't index from before this time")
	notAfter := flag.Int64("foreground", 2147483648, "Unix Timestamp. Don't index from after this time")
	flag.Parse()

	esIndex(time.Unix(*notBefore, 0), time.Unix(*notAfter, 0))
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
                "Duration":       { "type": "float" },
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
		Index("cluegetter-" + sess.DateConnect.Format("20060102")).
		Type("session").
		Id(id).
		BodyString(string(str)).
		Do()

	if err != nil {
		Log.Error("Could not index session '%s', error: %s", id, err.Error())
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

func esIndex(notBefore, notAfter time.Time) {
	esStart()
	rdbmsStart()

	var idTmp []byte
	err := Rdbms.QueryRow(`
			SELECT SQL_NO_CACHE id FROM session ORDER BY id DESC LIMIT 1`,
	).Scan(&idTmp)
	if err != nil {
		Log.Fatal(err)
	}
	var id [16]byte
	copy(id[:], idTmp[0:16])
	Log.Debug("First upper boundary id: '%s'", hex.EncodeToString(id[:]))

	rawSessions := make(chan map[[16]byte]*milterSession, 10)
	rawMessages := make(chan map[string]*Message, 10)
	completedSessions := make(chan *milterSession, 1024)

	wg := sync.WaitGroup{}
	wg.Add(3)

	for w := 1; w <= 128; w++ {
		go esPersistSessions(completedSessions)
	}

	go func() {
		for messages := range rawMessages {
			wg2 := sync.WaitGroup{}
			wg2.Add(3)
			go func() {
				esHydrateRecipientsFromDb(messages)
				wg2.Done()
			}()
			go func() {
				esHydrateHeadersFromDb(messages)
				wg2.Done()
			}()
			go func() {
				esHydrateCheckResultsFromDb(messages)
				wg2.Done()
			}()
			wg2.Wait()
			for _, msg := range messages {
				completedSessions <- msg.session
			}
		}
		close(completedSessions)
		wg.Done()
	}()

	go func() {
		for sessions := range rawSessions {
			if len(sessions) == 0 {
				continue
			}
			esHydrateMessagesFromDb(sessions, rawMessages)
		}
		close(rawMessages)
		wg.Done()
	}()

	go func() {
		esProcesSessionsFromDb(id, rawSessions, notBefore, notAfter)
		close(rawSessions)
		wg.Done()
	}()
	wg.Wait()
}

func esPersistSessions(sessions <-chan *milterSession) {
	for sess := range sessions {
		esSaveSession(sess)
	}
}

func esProcesSessionsFromDb(id [16]byte, rawSessions chan map[[16]byte]*milterSession, notBefore, notAfter time.Time) {
	bar := pb.StartNew(rdbmsRowsInTable("session"))
	t0 := time.Now()
	var count int
	id, count = esFetchSessionsFromDb(rawSessions, id[:], t0, true, notBefore, notAfter)
	for {
		if count == 0 {
			return
		}

		bar.Add(count)
		id, count = esFetchSessionsFromDb(rawSessions, id[:], t0, false, notBefore, notAfter)
	}
}

func esFetchSessionsFromDb(rawSessions chan map[[16]byte]*milterSession, startAtId []byte, before time.Time,
	includeId bool, notBefore, notAfter time.Time) ([16]byte, int) {
	var compare = "<"
	if includeId {
		compare = "<="
	}
	sql := `SELECT s.id, s.cluegetter_instance, s.date_connect, s.date_disconnect,
	 			s.ip, s.reverse_dns, s.helo, s.sasl_username, s.sasl_method,
	 			s.cert_issuer, s.cert_subject, s.cipher_bits, s.cipher,
	 			s.tls_version, cc.hostname, cc.daemon_name
			FROM session s JOIN cluegetter_client cc ON cc.id = s.cluegetter_client
			WHERE s.id ` + compare + ` ? AND s.date_connect < ? ORDER BY s.id DESC LIMIT 0,512`
	rows, err := Rdbms.Query(sql, startAtId, before)
	if err != nil {
		Log.Fatal(err)
	}
	defer rows.Close()

	var lastId [16]byte
	sessions := make(map[[16]byte]*milterSession, 0)
	count := 0
	for rows.Next() {
		sess := &milterSession{}
		var id []uint8
		var dateDisconnect NullTime
		if err := rows.Scan(&id, &sess.Instance, &sess.DateConnect, &dateDisconnect,
			&sess.Ip, &sess.ReverseDns, &sess.Helo, &sess.SaslUsername,
			&sess.SaslMethod, &sess.CertIssuer, &sess.CertSubject,
			&sess.CipherBits, &sess.Cipher, &sess.TlsVersion, &sess.MtaHostName,
			&sess.MtaDaemonName,
		); err != nil {
			Log.Error("Could not scan a session")
			continue
		}
		if dateDisconnect.Valid {
			sess.DateDisconnect = dateDisconnect.Time
		}
		copy(sess.id[:], id[0:16])
		lastId = sess.id

		if sess.DateConnect.After(notAfter) || sess.DateConnect.Before(notBefore) {
			continue
		}

		sessions[sess.id] = sess

		if len(sessions) > 64 {
			rawSessions <- sessions
			sessions = make(map[[16]byte]*milterSession, 0)
		}
		count = count + 1
	}

	rawSessions <- sessions
	return lastId, count
}

// TODO: MessageId
func esHydrateMessagesFromDb(sessions map[[16]byte]*milterSession, msgChan chan map[string]*Message) {
	sessionIds := make([]interface{}, 0)
	for sessId := range sessions {
		sessionIds = append(sessionIds, string(sessId[:]))
	}

	sql := `SELECT m.id, m.session, m.date, m.body_size, m.body_hash,
			m.sender_local, m.sender_domain, m.rcpt_count,
			m.verdict, m.verdict_msg, m.rejectScore, m.rejectScoreThreshold,
			m.tempfailScore, m.tempfailScoreThreshold
		 FROM message m
		 WHERE m.session IN (?` + strings.Repeat(",?", len(sessionIds)-1) + `)`
	rows, err := Rdbms.Query(sql, sessionIds...)
	if err != nil {
		Log.Fatal(err)
	}

	messages := make(map[string]*Message)
	for rows.Next() {
		msg := &Message{}
		var sender_local string
		var sender_domain string
		var rcptCount int
		var sessIdTmp []byte
		var verdict string
		if err := rows.Scan(&msg.QueueId, &sessIdTmp, &msg.Date, &msg.BodySize,
			&msg.BodyHash, &sender_local, &sender_domain, &rcptCount,
			&verdict, &msg.VerdictMsg, &msg.RejectScore,
			&msg.RejectScoreThreshold, &msg.TempfailScore,
			&msg.TempfailScoreThreshold,
		); err != nil {
			Log.Error("Could not scan a message")
			continue
		}

		var sessId [16]byte
		copy(sessId[:], sessIdTmp[0:16])
		msg.From = address.FromString(sender_local + "@" + sender_domain)
		msg.Verdict = int(Proto_Message_Verdict_value[verdict])

		sessions[sessId].Messages = append(sessions[sessId].Messages, msg)
		messages[msg.QueueId] = msg
		msg.session = sessions[sessId]
	}
	rows.Close()

	if len(messages) > 0 {
		msgChan <- messages
	}
}

func esHydrateRecipientsFromDb(messages map[string]*Message) {
	msgIds := make([]interface{}, 0)
	for msgId := range messages {
		msgIds = append(msgIds, msgId)
	}

	sql := `SELECT mr.message, mr.count, r.local, r.domain
			FROM message_recipient mr
			JOIN recipient r ON r.id = mr.recipient
			WHERE mr.message IN (?` + strings.Repeat(",?", len(msgIds)-1) + `)`

	rows, err := Rdbms.Query(sql, msgIds...)
	if err != nil {
		Log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var msgId string
		var count int
		var local string
		var domain string
		if err := rows.Scan(&msgId, &count, &local, &domain); err != nil {
			Log.Error("Could not scan a recipient")
			continue
		}

		rcpt := address.FromString(local + "@" + domain)
		for i := 0; i < count; i++ {
			messages[msgId].Rcpt = append(messages[msgId].Rcpt, rcpt)
		}
	}
}

func esHydrateHeadersFromDb(messages map[string]*Message) {
	msgIds := make([]interface{}, 0)
	for msgId := range messages {
		msgIds = append(msgIds, msgId)
	}

	sql := `SELECT mh.message, mh.name, mh.body
			FROM message_header mh
			WHERE mh.message IN (?` + strings.Repeat(",?", len(msgIds)-1) + `)`

	rows, err := Rdbms.Query(sql, msgIds...)
	if err != nil {
		Log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var msgId string
		hdr := MessageHeader{}
		if err := rows.Scan(&msgId, &hdr.Key, &hdr.Value); err != nil {
			Log.Error("Could not scan a header")
			continue
		}

		messages[msgId].Headers = append(messages[msgId].Headers, hdr)
	}
}

func esHydrateCheckResultsFromDb(messages map[string]*Message) {
	msgIds := make([]interface{}, 0)
	for msgId := range messages {
		msgIds = append(msgIds, msgId)
	}

	sql := `SELECT mr.message, mr.module, mr.verdict, mr.score,
			mr.weighted_score, mr.duration, mr.determinants
		FROM message_result mr
		WHERE mr.message IN (?` + strings.Repeat(",?", len(msgIds)-1) + `)`

	rows, err := Rdbms.Query(sql, msgIds...)
	if err != nil {
		Log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var msgId string
		var verdict string
		var duration float64
		var determinants []byte
		res := MessageCheckResult{}
		if err := rows.Scan(&msgId, &res.module, &verdict, &res.score,
			&res.weightedScore, &duration, &determinants,
		); err != nil {
			Log.Error("Could not scan a check result")
			continue
		}

		json.Unmarshal(determinants, &res.determinants)
		res.duration, _ = time.ParseDuration(fmt.Sprintf("%fs", duration))
		res.suggestedAction = int(Proto_Message_Verdict_value[verdict])
		messages[msgId].CheckResults = append(messages[msgId].CheckResults, &res)
	}
}
