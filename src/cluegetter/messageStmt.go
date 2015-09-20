package main

import (
	"database/sql"
)

var MessageStmtInsertMsg = *new(*sql.Stmt)
var MessageStmtInsertMsgBody = *new(*sql.Stmt)
var MessageStmtInsertRcpt = *new(*sql.Stmt)
var MessageStmtInsertMsgRcpt = *new(*sql.Stmt)
var MessageStmtInsertMsgHdr = *new(*sql.Stmt)
var MessageStmtSetVerdict = *new(*sql.Stmt)
var MessageStmtInsertModuleResult = *new(*sql.Stmt)
var MessageStmtPruneBody = *new(*sql.Stmt)
var MessageStmtPruneHeader = *new(*sql.Stmt)
var MessageStmtPruneMessageResult = *new(*sql.Stmt)
var MessageStmtPruneMessageQuota = *new(*sql.Stmt)
var MessageStmtPruneMessage = *new(*sql.Stmt)
var MessageStmtPruneMessageRecipient = *new(*sql.Stmt)
var MessageStmtPruneRecipient = *new(*sql.Stmt)
var MessageStmtPruneSession = *new(*sql.Stmt)

func messageStmtStart() {

	stmt, err := Rdbms.Prepare(`
		INSERT INTO message (id, session, date, body_size, body_hash, messageId,
			sender_local, sender_domain, rcpt_count) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtInsertMsg = stmt

	MessageStmtInsertMsgBody, err = Rdbms.Prepare(`INSERT INTO message_body(message, sequence, body) VALUES(?, ?, ?)
								ON DUPLICATE KEY UPDATE message=LAST_INSERT_ID(message)`)
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

	MessageStmtSetVerdict, err = Rdbms.Prepare(`UPDATE message SET verdict=?, verdict_msg=?, rejectScore=?, tempfailScore=? WHERE id=?`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtInsertModuleResult, err = Rdbms.Prepare(`INSERT INTO message_result (message, module, verdict,
								score, duration, determinants) VALUES(?, ?, ?, ?, ?, ?)`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneBody, err = Rdbms.Prepare(`
		DELETE FROM message_body WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (DATE(?) - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneHeader, err = Rdbms.Prepare(`
		DELETE FROM message_header WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (DATE(?) - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneMessageResult, err = Rdbms.Prepare(`
		DELETE FROM message_result WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (DATE(?) - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneMessageQuota, err = Rdbms.Prepare(`
		DELETE FROM quota_message WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (DATE(?) - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneMessage, err = Rdbms.Prepare(`
		DELETE FROM message
		WHERE date < (DATE(?) - INTERVAL ? WEEK)
			AND session IN
				(SELECT id FROM session s WHERE s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneMessageRecipient, err = Rdbms.Prepare(`
		DELETE FROM message_recipient WHERE message IN
			(SELECT m.id FROM message m
				LEFT JOIN session s ON s.id = m.session
			 WHERE m.date < (DATE(?) - INTERVAL ? WEEK) AND
				s.cluegetter_instance = ?)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneRecipient, err = Rdbms.Prepare(`
		DELETE FROM recipient WHERE NOT EXISTS
			(SELECT ?,?,? FROM message_recipient mr WHERE mr.recipient = recipient.id)
		`)
	if err != nil {
		Log.Fatal(err)
	}

	MessageStmtPruneSession, err = Rdbms.Prepare(`
		DELETE FROM session WHERE NOT EXISTS
			(SELECT * FROM message m WHERE m.session = session.id)
			AND date_connect < (DATE(?) - INTERVAL ? WEEK) AND cluegetter_instance = ?
		`)
	if err != nil {
		Log.Fatal(err)
	}

}
