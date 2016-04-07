package main

import (
	"crypto/md5"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	MessageStmtInsertMsg             = *new(*sql.Stmt)
	MessageStmtInsertMsgBody         = *new(*sql.Stmt)
	MessageStmtInsertRcpt            = *new(*sql.Stmt)
	MessageStmtInsertMsgRcpt         = *new(*sql.Stmt)
	MessageStmtInsertMsgHdr          = *new(*sql.Stmt)
	MessageStmtInsertModuleResult    = *new(*sql.Stmt)
	MessageStmtPruneBody             = *new(*sql.Stmt)
	MessageStmtPruneHeader           = *new(*sql.Stmt)
	MessageStmtPruneMessageResult    = *new(*sql.Stmt)
	MessageStmtPruneMessageQuota     = *new(*sql.Stmt)
	MessageStmtPruneMessage          = *new(*sql.Stmt)
	MessageStmtPruneMessageRecipient = *new(*sql.Stmt)
	MessageStmtPruneRecipient        = *new(*sql.Stmt)
	MessageStmtPruneSession          = *new(*sql.Stmt)

	messagePersistQueue = make(chan []byte, 100)
)

func messagePersistStart() {
	messagePersistStmtPrepare()

	messagePersistQueue = make(chan []byte)
	in := make(chan []byte)
	redisListSubscribe("cluegetter-"+strconv.Itoa(int(instance))+"-message-persist", messagePersistQueue, in)
	go messagePersistHandleQueue(in)

	if Config.ClueGetter.Archive_Prune_Interval != 0 {
		go messagePersistPrune()
	} else {
		Log.Info("archive-prune-interval set to 0. Not pruning anything.")
	}

	ticker := time.NewTicker(time.Second * 30)
	go func() {
		for range ticker.C {
			messagePersistCache.prune()
		}
	}()
}

func messagePersistHandleQueue(queue chan []byte) {
	for {
		data := <-queue
		messagePersistProtoBuf(data)
	}
}

func messagePersistProtoBuf(protoBuf []byte) {
	defer func() {
		if Config.ClueGetter.Exit_On_Panic {
			return
		}
		r := recover()
		if r == nil {
			return
		}
		Log.Error("Panic caught in messagePersistProtoBuf(). Recovering. Error: %s", r)
		StatsCounters["MessagePanics"].increase(1)
		return
	}()

	msg, err := messagePersistUnmarshalProto(protoBuf)
	if err != nil {
		panic("unmarshaling error: " + err.Error())
	}

	messagePersist(msg)
}

func messagePersistUnmarshalProto(protoBuf []byte) (*Proto_Message, error) {
	msg := &Proto_Message{}
	err := msg.Unmarshal(protoBuf)
	if err != nil {
		return nil, errors.New("Error unmarshalling message: " + err.Error())
	}

	return msg, nil
}

func messagePersistStmtPrepare() {
	stmt, err := Rdbms.Prepare(`
		INSERT INTO message (id, session, date, body_size, body_hash, messageId,
			sender_local, sender_domain, rcpt_count, verdict, verdict_msg,
			rejectScore, rejectScoreThreshold, tempfailScore, tempfailScoreThreshold
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtInsertMsg = stmt

	MessageStmtInsertMsgBody, err = Rdbms.Prepare(`INSERT INTO message_body(message, sequence, body) VALUES(?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtInsertRcpt, err = Rdbms.Prepare(`INSERT INTO recipient(local, domain) VALUES(?, ?)
								ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtInsertMsgRcpt, err = Rdbms.Prepare(`INSERT IGNORE INTO message_recipient(message, recipient, count) VALUES(?, ?,1)
								ON DUPLICATE KEY UPDATE count=count+1`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtInsertMsgHdr, err = Rdbms.Prepare(`INSERT INTO message_header(message, name, body) VALUES(?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtInsertModuleResult, err = Rdbms.Prepare(`INSERT INTO message_result (message, module, verdict,
								score, weighted_score, duration, determinants) VALUES(?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneBody, err = Rdbms.Prepare(`
		DELETE mb FROM message_body mb
				LEFT JOIN message m ON m.id = mb.message
				LEFT JOIN session s ON s.id = m.session
			WHERE m.date < (? - INTERVAL ? WEEK)
				AND s.cluegetter_instance = ?;
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneHeader, err = Rdbms.Prepare(`
		DELETE FROM message_header WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (? - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneMessageResult, err = Rdbms.Prepare(`
		DELETE FROM message_result WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (? - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneMessageQuota, err = Rdbms.Prepare(`
		DELETE FROM quota_message WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (? - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneMessage, err = Rdbms.Prepare(`
		DELETE m FROM message m
			INNER JOIN session s ON s.id = m.session
			WHERE m.date < (? - INTERVAL ? WEEK)
				AND s.cluegetter_instance = ?
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneMessageRecipient, err = Rdbms.Prepare(`
		DELETE FROM message_recipient WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (? - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneRecipient, err = Rdbms.Prepare(`
		DELETE r FROM recipient r
			LEFT JOIN message_recipient mr ON mr.recipient = r.id
			WHERE mr.recipient IS NULL AND (1 OR ? OR ? OR ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneSession, err = Rdbms.Prepare(`
		DELETE s FROM session s
			LEFT JOIN message m ON m.session = s.id
			WHERE s.date_connect < (? - INTERVAL ? WEEK)
				AND s.cluegetter_instance = ?
				AND m.id IS NULL
		`)
	if err != nil {
		Log.Fatal(err)
	}

}

func messagePersistPrune() {
	ticker := time.NewTicker(time.Duration(Config.ClueGetter.Archive_Prune_Interval) * time.Second)

	var prunables = []struct {
		stmt      *sql.Stmt
		descr     string
		retention float64
	}{
		{MessageStmtPruneBody, "bodies", Config.ClueGetter.Archive_Retention_Body},
		{MessageStmtPruneHeader, "headers", Config.ClueGetter.Archive_Retention_Header},
		{MessageStmtPruneMessageResult, "message results", Config.ClueGetter.Archive_Retention_Message_Result},
		{MessageStmtPruneMessageQuota, "message-quota relations", Config.ClueGetter.Archive_Retention_Message},
		{MessageStmtPruneMessageRecipient, "message-recipient relations", Config.ClueGetter.Archive_Retention_Message},
		{MessageStmtPruneMessage, "messages", Config.ClueGetter.Archive_Retention_Message},
		{MessageStmtPruneSession, "sessions", Config.ClueGetter.Archive_Retention_Message},
		{MessageStmtPruneRecipient, "recipients", Config.ClueGetter.Archive_Retention_Message},
	}

WaitForNext:
	for {
		select {
		case <-ticker.C:
			t0 := time.Now()
			Log.Info("Pruning some old data now")

			for _, prunable := range prunables {
				if prunable.retention < Config.ClueGetter.Archive_Retention_Safeguard {
					Log.Info("Not pruning %s because its retention (%.2f weeks) is lower than the safeguard (%.2f)",
						prunable.descr, prunable.retention, Config.ClueGetter.Archive_Retention_Safeguard)
					continue
				}

				tStart := time.Now()
				res, err := prunable.stmt.Exec(t0, prunable.retention, instance)
				if err != nil {
					Log.Error("Could not prune %s: %s", prunable.descr, err.Error())
					continue WaitForNext
				}

				rowCnt, err := res.RowsAffected()
				if err != nil {
					Log.Error("Error while fetching number of affected rows: ", err)
					continue WaitForNext
				}

				Log.Info("Pruned %d %s in %s", rowCnt, prunable.descr, time.Now().Sub(tStart).String())
			}
		}
	}
}

func messagePersist(msg *Proto_Message) {
	sess := *msg.Session
	milterSessionPersist(&sess)

	var sender_local, sender_domain string
	if strings.Index(*msg.From, "@") != -1 {
		sender_local = strings.Split(*msg.From, "@")[0]
		sender_domain = strings.Split(*msg.From, "@")[1]
	} else {
		sender_local = *msg.From
	}

	messageIdHdr := ""
	for _, v := range msg.Headers {
		if strings.EqualFold((*v).GetKey(), "Message-Id") {
			messageIdHdr = (*v).GetValue()
		}
	}

	verdictValue := [3]string{"permit", "tempfail", "reject"}
	StatsCounters["RdbmsQueries"].increase(1)
	sessId := sess.GetId()
	_, err := MessageStmtInsertMsg.Exec(
		msg.Id,
		string(sessId[:]),
		time.Now(),
		len(msg.Body),
		fmt.Sprintf("%x", md5.Sum(msg.Body)),
		messageIdHdr,
		sender_local,
		sender_domain,
		len(msg.Rcpt),
		verdictValue[*msg.Verdict],
		msg.VerdictMsg,
		msg.RejectScore,
		msg.RejectScoreThreshold,
		msg.TempfailScore,
		msg.TempfailScoreThreshold,
	)

	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
	}

	if Config.ClueGetter.Archive_Retention_Message_Result > 0 {
		messageSaveCheckResults(msg)
	}

	if Config.ClueGetter.Archive_Retention_Body > 0 {
		messageSaveBody(msg)
	}

	messageSaveRecipients(msg.Rcpt, msg.Id)
	if Config.ClueGetter.Archive_Retention_Header > 0 {
		messageSaveHeaders(msg)
	}

}

func messageSaveCheckResults(msg *Proto_Message) {
	for _, result := range msg.CheckResults {

		StatsCounters["RdbmsQueries"].increase(1)
		_, err := MessageStmtInsertModuleResult.Exec(
			msg.Id, result.Module, result.Verdict.String(),
			result.Score, result.WeightedScore, result.Duration, result.Determinants)
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
		}
	}
}

func messageSaveHeaders(msg *Proto_Message) {
	for _, headerPair := range msg.Headers {
		StatsCounters["RdbmsQueries"].increase(1)
		_, err := MessageStmtInsertMsgHdr.Exec(
			msg.Id, (*headerPair).GetKey(), (*headerPair).GetValue())

		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
		}
	}
}

/**
 * Store message in chunks of 65K bytes
 */
func messageSaveBody(msg *Proto_Message) {
	for i := 0; (i * 65535) < len(msg.Body); i++ {
		StatsCounters["RdbmsQueries"].increase(1)
		boundary := (i + 1) * 65535
		if boundary > len(msg.Body) {
			boundary = len(msg.Body)
		}

		_, err := MessageStmtInsertMsgBody.Exec(
			msg.Id,
			i,
			msg.Body[(i*65535):boundary],
		)

		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
		}
	}
}

func messageSaveRecipients(recipients []string, msgId *string) {
	for _, rcpt := range recipients {
		var local string
		var domain string

		if strings.Index(rcpt, "@") != -1 {
			local = strings.SplitN(rcpt, "@", 2)[0]
			domain = strings.SplitN(rcpt, "@", 2)[1]
		} else {
			local = rcpt
			domain = ""
		}

		StatsCounters["RdbmsQueries"].increase(1)
		res, err := MessageStmtInsertRcpt.Exec(local, domain)
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			panic("Could not execute MessageStmtInsertRcpt in messageSaveRecipients(). Error: " + err.Error())
		}

		rcptId, err := res.LastInsertId()
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			panic("Could not get lastinsertid from MessageStmtInsertRcpt in messageSaveRecipients(). Error: " + err.Error())
		}

		StatsCounters["RdbmsQueries"].increase(1)
		_, err = MessageStmtInsertMsgRcpt.Exec(msgId, rcptId)
		if err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			panic("Could not get execute MessageStmtInsertMsgRcpt in messageSaveRecipients(). Error: " + err.Error())
		}
	}
}

func messagePersistInCache(queueId string, msgId string, msg []byte) {
	if ok, err := messagePersistCache.Set(queueId, msgId, msg); !ok {
		Log.Notice("Could not add message %s to message cache: %s",
			queueId, err.Error())
	}
}

///////////////////
// Message Cache //
///////////////////

var messagePersistCache = messageCacheNew(5*1024*1024, 512*1024*1024)

type messageCache struct {
	sync.RWMutex
	cache        map[string][]byte
	age          map[string]int64
	msgIdIdx     map[string]string
	msgIdRevIdx  map[string]string
	size         uint64
	maxSize      uint64
	maxEntrySize uint64
}

func messageCacheNew(maxEntrySize uint64, maxSize uint64) *messageCache {
	return &messageCache{
		cache:        make(map[string][]byte),
		age:          make(map[string]int64),
		msgIdIdx:     make(map[string]string),
		msgIdRevIdx:  make(map[string]string),
		size:         0,
		maxSize:      maxSize,
		maxEntrySize: maxEntrySize,
	}
}

func (c *messageCache) getByQueueId(queueId string) []byte {
	c.RLock()
	defer c.RUnlock()

	return c.cache[queueId]
}

func (c *messageCache) getByMessageId(msgId string) []byte {
	c.RLock()
	queueId := c.msgIdIdx[msgId]
	protoBuf := c.cache[queueId]
	c.RUnlock()

	return protoBuf
}

func (c *messageCache) Set(queueId, msgId string, msg []byte) (bool, error) {
	size := uint64(len(msg))
	if size > c.maxEntrySize {
		return false, errors.New(fmt.Sprintf(
			"Could not cache item '%s' (size %d). Item is bigger than max entry size %d",
			queueId, size, c.maxEntrySize,
		))
	}

	c.Lock()
	defer c.Unlock()

	if c.size+size > c.maxSize {
		return false, errors.New(fmt.Sprintf(
			"Could not cache item '%s' (size %d). Total cache size would be exceeded.",
			queueId, size,
		))
	}

	if _, exists := c.cache[queueId]; exists {
		c._del(queueId)
	}

	c.msgIdIdx[msgId] = queueId
	c.msgIdRevIdx[queueId] = msgId

	c.cache[queueId] = msg
	c.age[queueId] = time.Now().Unix()
	atomic.AddUint64(&c.size, size)

	return true, nil
}

func (c *messageCache) _del(id string) {
	item, exists := c.cache[id]
	if !exists {
		return
	}

	size := len(item)
	delete(c.cache, id)
	delete(c.age, id)
	atomic.AddUint64(&c.size, ^uint64(size-1))

	delete(c.msgIdIdx, c.msgIdRevIdx[id])
	delete(c.msgIdRevIdx, id)
}

func (c *messageCache) prune() {
	if c.size < c.maxSize/2 {
		Log.Debug("Skipping pruning of messageCache it's below 50%% (%d/%d) capacity.", c.size, c.maxSize)
		return
	}

	t0 := time.Now()
	cutoff := t0.Unix() - 300
	prune := make([]string, 0)
	c.RLock()
	for key, age := range c.age {
		if age < cutoff {
			prune = append(prune, key)
		}
	}
	c.RUnlock()
	Log.Debug("Scanned for prunable message cache items in %s", time.Now().Sub(t0).String())
	t0 = time.Now()
	if len(prune) == 0 {
		goto end
	}

	c.Lock()
	defer c.Unlock()

	for _, key := range prune {
		c._del(key)
	}
end:
	Log.Debug("Pruned %d message cache items in %s. It now contains %d items, %d bytes",
		len(prune), time.Now().Sub(t0).String(), len(c.cache), c.size)
}
