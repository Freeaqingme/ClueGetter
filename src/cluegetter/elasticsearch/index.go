// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package elasticsearch

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"cluegetter/address"
	"cluegetter/core"

	"flag"
	"github.com/Freeaqingme/GoDaemonSkeleton"
	"gopkg.in/cheggaaa/pb.v1"
	"os"
)

func init() {
	handover := subApp
	GoDaemonSkeleton.AppRegister(&GoDaemonSkeleton.App{
		Name:     ModuleName,
		Handover: &handover,
	})
}

func subApp() {
	if len(os.Args) <= 1 || os.Args[1] != "index" {
		fmt.Println("SubApp 'elasticsearch' requires a second argument. Must be one of: index")
		os.Exit(1)
	}

	os.Args = os.Args[1:]
	notBefore := flag.Int64("notBefore", 0, "Unix Timestamp. Don't index from before this time")
	notAfter := flag.Int64("notAfter", 2147483648, "Unix Timestamp. Don't index from after this time. Defaults to 2038")
	flag.Parse()

	module := &Module{BaseModule: core.NewBaseModule(core.InitCg())}
	module.Init()

	module.index(time.Unix(*notBefore, 0), time.Unix(*notAfter, 0))
}

func (m *Module) persistSessionChan(sessions <-chan *core.MilterSession) {
	for sess := range sessions {
		m.persistSession(sess)
	}
}

func (m *Module) index(notBefore, notAfter time.Time) {
	var id []byte
	err := m.Rdbms().QueryRow(`
			SELECT SQL_NO_CACHE id FROM session ORDER BY id DESC LIMIT 1`,
	).Scan(&id)
	if err != nil {
		m.Log().Fatalf("%s", err)
	}
	m.Log().Debugf("First upper boundary id: '%s'", hex.EncodeToString(id))

	rawSessions := make(chan map[[16]byte]*core.MilterSession, 10)
	rawMessages := make(chan map[string]*core.Message, 10)
	completedSessions := make(chan *core.MilterSession, 1024)

	wg := sync.WaitGroup{}
	wg.Add(3)

	for w := 1; w <= 128; w++ {
		go m.persistSessionChan(completedSessions)
	}

	go func() {
		for messages := range rawMessages {
			wg2 := sync.WaitGroup{}
			wg2.Add(3)
			go func() {
				indexHydrateRecipientsFromDb(m, messages)
				wg2.Done()
			}()
			go func() {
				indexHydrateHeadersFromDb(m, messages)
				wg2.Done()
			}()
			go func() {
				indexHydrateCheckResultsFromDb(m, messages)
				wg2.Done()
			}()
			wg2.Wait()
			for _, msg := range messages {
				completedSessions <- msg.Session()
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
			indexHydrateMessagesFromDb(m, sessions, rawMessages)
		}
		close(rawMessages)
		wg.Done()
	}()

	go func() {
		indexSessionsFromDb(m, id, rawSessions, notBefore, notAfter)
		close(rawSessions)
		wg.Done()
	}()
	wg.Wait()
}

func indexSessionsFromDb(m *Module, id []byte, rawSessions chan map[[16]byte]*core.MilterSession,
	notBefore, notAfter time.Time) {
	bar := pb.StartNew(core.RdbmsRowsInTable("session"))
	t0 := time.Now()
	var count int
	id, count = indexFetchSessionsFromDb(m, rawSessions, id, t0, true, notBefore, notAfter)
	for {
		if count == 0 {
			return
		}

		bar.Add(count)
		id, count = indexFetchSessionsFromDb(m, rawSessions, id, t0, false, notBefore, notAfter)
	}
}

func indexFetchSessionsFromDb(m *Module, rawSessions chan map[[16]byte]*core.MilterSession, startAtId []byte, before time.Time,
	includeId bool, notBefore, notAfter time.Time) ([]byte, int) {
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
	rows, err := m.Rdbms().Query(sql, startAtId, before)
	if err != nil {
		m.Log().Fatalf("%s", err)
	}
	defer rows.Close()

	var lastId [16]byte
	sessions := make(map[[16]byte]*core.MilterSession, 0)
	count := 0
	for rows.Next() {
		sess := &core.MilterSession{}
		var id []uint8
		var dateDisconnect core.NullTime
		if err := rows.Scan(&id, &sess.Instance, &sess.DateConnect, &dateDisconnect,
			&sess.Ip, &sess.ReverseDns, &sess.Helo, &sess.SaslUsername,
			&sess.SaslMethod, &sess.CertIssuer, &sess.CertSubject,
			&sess.CipherBits, &sess.Cipher, &sess.TlsVersion, &sess.MtaHostName,
			&sess.MtaDaemonName,
		); err != nil {
			m.Log().Errorf("Could not scan a session")
			continue
		}
		if dateDisconnect.Valid {
			sess.DateDisconnect = dateDisconnect.Time
		}
		var idArray [16]byte
		copy(idArray[:], id[:16])
		sess.SetId(idArray)
		lastId = sess.IdArray()

		if sess.DateConnect.After(notAfter) || sess.DateConnect.Before(notBefore) {
			continue
		}

		sessions[sess.IdArray()] = sess

		if len(sessions) > 64 {
			rawSessions <- sessions
			sessions = make(map[[16]byte]*core.MilterSession, 0)
		}
		count = count + 1
	}

	rawSessions <- sessions
	return lastId[:], count
}

func indexHydrateMessagesFromDb(m *Module, sessions map[[16]byte]*core.MilterSession, msgChan chan map[string]*core.Message) {
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
	rows, err := m.Rdbms().Query(sql, sessionIds...)
	if err != nil {
		m.Log().Fatalf("%s", err)
	}

	messages := make(map[string]*core.Message)
	for rows.Next() {
		msg := &core.Message{}
		var sender_local string
		var sender_domain string
		var rcptCount int
		var sessId []byte
		var verdict string
		if err := rows.Scan(&msg.QueueId, &sessId, &msg.Date, &msg.BodySize,
			&msg.BodyHash, &sender_local, &sender_domain, &rcptCount,
			&verdict, &msg.VerdictMsg, &msg.RejectScore,
			&msg.RejectScoreThreshold, &msg.TempfailScore,
			&msg.TempfailScoreThreshold,
		); err != nil {
			m.Log().Errorf("Could not scan a message")
			continue
		}

		msg.From = address.FromString(sender_local + "@" + sender_domain)
		msg.Verdict = int(core.Proto_Message_Verdict_value[verdict])

		sessions[idArray(sessId)].Messages = append(sessions[idArray(sessId)].Messages, msg)
		messages[msg.QueueId] = msg
		msg.SetSession(sessions[idArray(sessId)])
	}
	rows.Close()

	if len(messages) > 0 {
		msgChan <- messages
	}
}

func indexHydrateRecipientsFromDb(m *Module, messages map[string]*core.Message) {
	msgIds := make([]interface{}, 0)
	for msgId := range messages {
		msgIds = append(msgIds, msgId)
	}

	sql := `SELECT mr.message, mr.count, r.local, r.domain
			FROM message_recipient mr
			JOIN recipient r ON r.id = mr.recipient
			WHERE mr.message IN (?` + strings.Repeat(",?", len(msgIds)-1) + `)`

	rows, err := m.Rdbms().Query(sql, msgIds...)
	if err != nil {
		m.Log().Fatalf("%s", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msgId string
		var count int
		var local string
		var domain string
		if err := rows.Scan(&msgId, &count, &local, &domain); err != nil {
			m.Log().Errorf("Could not scan a recipient")
			continue
		}

		rcpt := address.FromString(local + "@" + domain)
		for i := 0; i < count; i++ {
			messages[msgId].Rcpt = append(messages[msgId].Rcpt, rcpt)
		}
	}
}

func indexHydrateHeadersFromDb(m *Module, messages map[string]*core.Message) {
	msgIds := make([]interface{}, 0)
	for msgId := range messages {
		msgIds = append(msgIds, msgId)
	}

	sql := `SELECT mh.message, mh.name, mh.body
			FROM message_header mh
			WHERE mh.message IN (?` + strings.Repeat(",?", len(msgIds)-1) + `)`

	rows, err := m.Rdbms().Query(sql, msgIds...)
	if err != nil {
		m.Log().Fatalf("%s", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msgId string
		hdr := core.MessageHeader{}
		if err := rows.Scan(&msgId, &hdr.Key, &hdr.Value); err != nil {
			m.Log().Errorf("Could not scan a header")
			continue
		}

		messages[msgId].Headers = append(messages[msgId].Headers, hdr)
	}
}

func indexHydrateCheckResultsFromDb(m *Module, messages map[string]*core.Message) {
	msgIds := make([]interface{}, 0)
	for msgId := range messages {
		msgIds = append(msgIds, msgId)
	}

	sql := `SELECT mr.message, mr.module, mr.verdict, mr.score,
			mr.weighted_score, mr.duration*1000000000, mr.determinants
		FROM message_result mr
		WHERE mr.message IN (?` + strings.Repeat(",?", len(msgIds)-1) + `)`

	rows, err := m.Rdbms().Query(sql, msgIds...)
	if err != nil {
		m.Log().Fatalf("%s", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msgId string
		var verdict string
		var duration float64
		var determinants []byte
		res := core.MessageCheckResult{}
		if err := rows.Scan(&msgId, &res.Module, &verdict, &res.Score,
			&res.WeightedScore, &duration, &determinants,
		); err != nil {
			m.Log().Errorf("Could not scan a check result")
			continue
		}

		json.Unmarshal(determinants, &res.Determinants)
		res.Duration, _ = time.ParseDuration(fmt.Sprintf("%fs", duration))
		res.SuggestedAction = int(core.Proto_Message_Verdict_value[verdict])
		messages[msgId].CheckResults = append(messages[msgId].CheckResults, &res)
	}
}

func idArray(sessId []byte) [16]byte {
	var sessIdArray [16]byte
	copy(sessIdArray[:], sessId[:16])
	return sessIdArray
}
