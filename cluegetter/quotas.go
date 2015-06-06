// GlueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package cluegetter

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type quotasSelectResultSet struct {
	id          uint64
	selector    string
	factorValue string
	period      uint32
	curb        uint32
	count       uint32
	msg_count   uint32
}

var QuotasSelectStmt = *new(*sql.Stmt)
var QuotaInsertQuotaMessageStmt = *new(*sql.Stmt)
var QuotaInsertDeducedQuotaStmt = *new(*sql.Stmt)

func quotasStart() {
	stmt, err := Rdbms.Prepare(quotasGetSelectQuery(nil))
	if err != nil {
		Log.Fatal(err)
	}
	QuotasSelectStmt = stmt

	stmt, err = Rdbms.Prepare(
		"INSERT INTO quota_message (quota, message) VALUES (?, ?) ON DUPLICATE KEY UPDATE message=message")
	if err != nil {
		Log.Fatal(err)
	}
	QuotaInsertQuotaMessageStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT INTO quota (selector, value, profile, instigator, date_added)
								SELECT DISTINCT q.selector, ?, q.profile, q.id, NOW() FROM quota q
								WHERE (q.selector = ? AND q.is_regex = 1 AND ? REGEXP q.value)
								ORDER by q.id ASC`)
	if err != nil {
		Log.Fatal(err)
	}
	QuotaInsertDeducedQuotaStmt = stmt

	Log.Info("Quotas module started successfully")
}

func quotasStop() {
	QuotasSelectStmt.Close()
	QuotaInsertQuotaMessageStmt.Close()
	QuotaInsertDeducedQuotaStmt.Close()

	Log.Info("Quotas module ended")
}

func quotasIsAllowed(msg Message) *MessageCheckResult {
	counts, err := quotasGetCounts(msg, true)
	if err != nil {
		Log.Error("Error in quotas module: %s", err)
		return &MessageCheckResult{
			suggestedAction: messageTempFail,
			message:         "An internal error occurred",
			score:           10,
		}
	}
	quotas := make(map[uint64]struct{})

	rcpt_count := msg.getRcptCount()
	for _, row := range counts {
		factorValue := row.factorValue
		quotas[row.id] = struct{}{}

		var future_total_count uint32
		var extra_count uint32
		if row.selector != "recipient" {
			future_total_count = row.count + uint32(rcpt_count)
			extra_count = uint32(rcpt_count)
		} else {
			future_total_count = row.msg_count + uint32(1)
			extra_count = uint32(1)
		}

		if future_total_count > row.curb {
			Log.Notice("Quota Exceeding, max of %d messages per %d seconds for %s '%s'",
				row.curb, row.period, row.selector, factorValue)
			msg := fmt.Sprintf("REJECT Policy reject; Exceeding quota, max of %d messages per %d seconds for %s '%s'",
				row.curb, row.period, row.selector, factorValue)
			return &MessageCheckResult{
				suggestedAction: messageTempFail,
				message:         msg,
				score:           100,
			}
		} else {
			Log.Info("Quota Updated, Adding %d message(s) to total of %d (max %d) for last %d seconds for %s '%s'",
				extra_count, row.count, row.curb, row.period, row.selector, factorValue)
		}
	}

	for quota_id := range quotas {
		StatsCounters["RdbmsQueries"].increase(1)
		_, err := QuotaInsertQuotaMessageStmt.Exec(quota_id, msg.getQueueId())
		if err != nil {
			Log.Fatal(err) // TODO
		}
	}

	return &MessageCheckResult{
		suggestedAction: messagePermit,
		message:         "",
		score:           1, // TODO: Does it make sense, having a score with messagePermit?
	}
}

func quotasGetCounts(msg Message, applyRegexes bool) ([]*quotasSelectResultSet, error) {
	rows, err := quotasGetCountsRaw(msg)
	defer rows.Close()

	results := []*quotasSelectResultSet{}
	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
		return results, err
	}

	factors := quotasGetMsgFactors(msg)

	for rows.Next() {
		r := new(quotasSelectResultSet)
		if err := rows.Scan(&r.id, &r.selector, &r.factorValue, &r.period,
			&r.curb, &r.count, &r.msg_count); err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
			return results, err
		}

		for factorValueKey, factorValue := range factors[r.selector] {
			if factorValue == r.factorValue {
				factors[r.selector][factorValueKey] = ""
			}
		}
		results = append(results, r)
	}

	if err = rows.Err(); err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
		return results, err
	}

	if !applyRegexes {
		return results, nil
	}

	for factorKey, factorValues := range factors {
		res := quotasGetRegexCounts(msg, factorKey, factorValues)
		if res != nil {
			results = append(results, res...)
		}
	}

	return results, nil
}

func quotasGetCountsRaw(msg Message) (*sql.Rows, error) {
	sess := *msg.getSession()
	factors := quotasGetMsgFactors(msg)

	StatsCounters["RdbmsQueries"].increase(1)
	_, hasFactorRecipient := factors["recipient"]
	if msg.getRcptCount() == 1 || !hasFactorRecipient {
		return QuotasSelectStmt.Query(
			msg.getQueueId(),
			msg.getFrom(),
			msg.getRecipients()[0],
			sess.getIp(),
			sess.getSaslUsername(),
		)
	}

	factorValueCounts := map[string]int{
		"recipient": msg.getRcptCount(),
	}

	query := quotasGetSelectQuery(factorValueCounts)
	queryArgs := make([]interface{}, 4+msg.getRcptCount())
	queryArgs[0] = interface{}(msg.getQueueId())
	queryArgs[1] = interface{}(msg.getFrom())
	i := 2
	for i = i; i < msg.getRcptCount()+2; i++ {
		queryArgs[i] = interface{}(msg.getRecipients()[i-2])
	}
	queryArgs[i] = interface{}(sess.getIp())
	queryArgs[i+1] = interface{}(sess.getSaslUsername())

	return Rdbms.Query(query, queryArgs...)
}

func quotasGetRegexCounts(msg Message, factor string, factorValues []string) []*quotasSelectResultSet {

	totalRowCount := int64(0)
	for _, factorValue := range factorValues {
		if factorValue == "" {
			continue
		}

		StatsCounters["RdbmsQueries"].increase(1)
		res, err := QuotaInsertDeducedQuotaStmt.Exec(factorValue, factor, factorValue)
		if err != nil {
			Log.Fatal(err) // TODO
		}
		rowCnt, err := res.RowsAffected()
		if err != nil {
			Log.Fatal(err)
		}
		totalRowCount = +rowCnt
	}

	if totalRowCount > 0 {
		counts, err := quotasGetCounts(msg, false)
		if err != nil {
			return nil
		}
		return counts
	}
	return nil
}

func quotasGetSelectQuery(factorValueCount map[string]int) string {
	pieces := []string{"(? IS NULL)", "(? IS NULL)", "(? IS NULL)", "(? IS NULL)"}
	factors := quotasGetFactors()
	index := map[string]int{
		"sender":         0,
		"recipient":      1,
		"client_address": 2,
		"sasl_username":  3,
	}

	for factor := range factors {
		if factorValueCount != nil && factorValueCount[factor] > 1 {
			pieces[index[factor]] = `(q.selector = '` + factor + `'  AND q.value IN
				(?` + strings.Repeat(",?", factorValueCount[factor]-1) + `))`
		} else {
			pieces[index[factor]] = "(q.selector = '" + factor + "'  AND q.value = ?)"
		}
	}

	if len(factors) == 0 {
		Log.Fatal("Quotas: No factors were given to account for.")
	}

	_, tzOffset := time.Now().Local().Zone()
	sql := fmt.Sprintf(`
		SELECT q.id, q.selector, q.value factorValue, pp.period, pp.curb,
			coalesce(sum(m.rcpt_count), 0) count, coalesce(count(m.rcpt_count), 0) msg_count
		FROM quota q
			LEFT JOIN quota_profile p         ON p.id = q.profile
			LEFT JOIN quota_profile_period pp ON p.id = pp.profile
			LEFT JOIN quota_message	qm        ON qm.quota = q.id AND qm.message != ?
			LEFT JOIN message m               ON m.id = qm.message
				AND m.date > FROM_UNIXTIME(UNIX_TIMESTAMP() - %d - pp.period)
		WHERE (`+strings.Join(pieces, " OR ")+`) AND q.is_regex = 0 AND p.cluegetter_instance = %d
			GROUP BY pp.id, q.id`, tzOffset, instance)
	return sql
}

func quotasGetFactors() map[string]struct{} {
	factors := make(map[string]struct{})

	if Config.Quotas.Account_Sender {
		factors["sender"] = struct{}{}
	}
	if Config.Quotas.Account_Recipient {
		factors["recipient"] = struct{}{}
	}
	if Config.Quotas.Account_Client_Address {
		factors["client_address"] = struct{}{}
	}
	if Config.Quotas.Account_Sasl_Username {
		factors["sasl_username"] = struct{}{}
	}

	return factors
}

func quotasGetMsgFactors(msg Message) map[string][]string {
	sess := *msg.getSession()
	factors := make(map[string][]string)

	if Config.Quotas.Account_Sender {
		factors["sender"] = []string{msg.getFrom()}
	}
	if Config.Quotas.Account_Recipient {
		factors["recipient"] = msg.getRecipients()
	}
	if Config.Quotas.Account_Client_Address {
		factors["client_address"] = []string{sess.getIp()}
	}
	if Config.Quotas.Account_Sasl_Username {
		factors["sasl_username"] = []string{sess.getSaslUsername()}
	}

	return factors
}
