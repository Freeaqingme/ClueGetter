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

func quotasIsAllowed(msg Message) string {
	counts, err := quotasGetCounts(msg)
	if err != nil {
		return ""
	}
	quotas := make(map[uint64]struct{})

	policy_count := msg.getRcptCount()
	for _, row := range counts {
		factorValue := row.factorValue
		quotas[row.id] = struct{}{}

		var future_total_count uint32
		var extra_count uint32
		if row.selector != "recipient" {
			future_total_count = row.count + uint32(policy_count)
			extra_count = uint32(policy_count)
		} else {
			future_total_count = row.msg_count + uint32(1)
			extra_count = uint32(1)
		}

		if future_total_count > row.curb {
			Log.Notice("Quota Exceeding, max of %d messages per %d seconds for %s '%s'",
				row.curb, row.period, row.selector, factorValue)
			return fmt.Sprintf("REJECT Policy reject; Exceeding quota, max of %d messages per %d seconds for %s '%s'",
				row.curb, row.period, row.selector, factorValue)
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

	return "DUNNO"
}

func quotasGetCounts(msg Message) ([]*quotasSelectResultSet, error) {
	rows, err := quotasGetCountsRaw(msg)
	results := []*quotasSelectResultSet{}
	factors := quotasGetFactors()

	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
		return results, err
	}

	defer rows.Close()

	for rows.Next() {
		r := new(quotasSelectResultSet)
		if err := rows.Scan(&r.id, &r.selector, &r.factorValue, &r.period,
			&r.curb, &r.count, &r.msg_count); err != nil {
			StatsCounters["RdbmsErrors"].increase(1)
			Log.Error(err.Error())
			return results, err
		}
		results = append(results, r)
		if _, ok := factors[r.selector]; ok {
			delete(factors, r.selector)
		}
	}

	if err = rows.Err(); err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error(err.Error())
		return results, err
	}

	if len(factors) > 0 {
		res := quotasGetRegexCounts(msg, factors)
		if res != nil {
			results = append(results, res...)
		}
	}

	return results, nil
}

func quotasGetCountsRaw(msg Message) (*sql.Rows, error) {
	sess := *msg.getSession()
	factors := quotasGetFactors()

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

func quotasGetRegexCounts(msg Message, factors map[string]struct{}) []*quotasSelectResultSet {
	// TODO?
	// If there's multiple factors, for which one or more no regex exist, semi-infinite recursion may occur. Yolo

	// Todo: Test this, especially in relation to (multiple) recipients
	totalRowCount := int64(0)
	for factor := range factors {
		factorValue := quotasGetFactorValue(msg, factor)

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
		delete(factors, factor)
	}

	if totalRowCount > 0 {
		counts, err := quotasGetCounts(msg)
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

	sql := `
		SELECT q.id, q.selector, q.value factorValue, pp.period, pp.curb,
			coalesce(sum(m.count), 0) count, coalesce(count(m.count), 0) msg_count
		FROM quota q
			LEFT JOIN quota_profile p         ON p.id = q.profile
			LEFT JOIN quota_profile_period pp ON p.id = pp.profile
			LEFT JOIN quota_message	qm        ON qm.quota = q.id AND qm.message != ?
			LEFT JOIN message m               ON m.id = qm.message AND m.date > FROM_UNIXTIME(UNIX_TIMESTAMP() - pp.period)
		WHERE (` + strings.Join(pieces, " OR ") + ") AND q.is_regex = 0 GROUP BY pp.id, q.id"
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

func quotasGetFactorValue(msg Message, factor string) string {
	sess := *msg.getSession()

	if factor == "sender" {
		return msg.getFrom()
	}
	if factor == "recipient" {
		return "" // TODO rcpt
	}
	if factor == "client_address" {
		return sess.getIp()
	}
	if factor == "sasl_username" {
		return sess.getSaslUsername()
	}

	panic("Do not know what to do with " + factor)
}
