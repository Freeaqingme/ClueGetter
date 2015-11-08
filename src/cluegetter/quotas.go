// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

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
	if Config.Quotas.Enabled != true {
		Log.Info("Skipping Quota module because it was not enabled in the config")
		return
	}

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

	stmt, err = Rdbms.Prepare(fmt.Sprintf(`
		INSERT INTO quota (selector, value, profile, instigator, date_added)
			SELECT DISTINCT q.selector, ?, q.profile, q.id, NOW() FROM quota q
			WHERE (q.selector = ? AND q.is_regex = 1 AND ? REGEXP q.value
				AND q.profile IN (
					SELECT qp.id FROM quota_profile qp LEFT JOIN quota_class qc ON qp.class = qc.id
						WHERE qc.cluegetter_instance = %d))
			ORDER by q.id ASC`, instance))
	if err != nil {
		Log.Fatal(err)
	}
	QuotaInsertDeducedQuotaStmt = stmt

	Log.Info("Quotas module started successfully")
}

func quotasStop() {
	if Config.Quotas.Enabled != true {
		return
	}

	QuotasSelectStmt.Close()
	QuotaInsertQuotaMessageStmt.Close()
	QuotaInsertDeducedQuotaStmt.Close()

	Log.Info("Quotas module ended")
}

func quotasIsAllowed(msg *Message, _ chan bool) *MessageCheckResult {
	counts, err := quotasGetCounts(msg, true)
	if err != nil {
		Log.Error("Error in quotas module: %s", err)
		return &MessageCheckResult{
			module:          "quotas",
			suggestedAction: messageTempFail,
			message:         "An internal error occurred",
			score:           100,
		}
	}
	quotas := make(map[uint64]struct{})

	rcpt_count := len(msg.Rcpt)
	rejectMsg := ""
	determinants := make(map[string]interface{})
	determinants["quotas"] = make([]interface{}, 0)
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

		determinant := make(map[string]interface{})
		determinant["curb"] = row.curb
		determinant["period"] = row.period
		determinant["selector"] = row.selector
		determinant["factorValue"] = row.factorValue
		determinant["extraCount"] = extra_count
		determinant["futureTotalCount"] = future_total_count
		determinants["quotas"] = append(determinants["quotas"].([]interface{}), determinant)

		if future_total_count > row.curb {
			Log.Notice("Quota Exceeding, max of %d messages per %d seconds for %s '%s'",
				row.curb, row.period, row.selector, factorValue)
			rejectMsg = fmt.Sprintf("REJECT Policy reject; Exceeding quota, max of %d messages per %d seconds for %s '%s'",
				row.curb, row.period, row.selector, factorValue)
		} else {
			Log.Info("Quota Updated, Adding %d message(s) to total of %d (max %d) for last %d seconds for %s '%s'",
				extra_count, row.count, row.curb, row.period, row.selector, factorValue)
		}
	}

	if rejectMsg != "" {
		return &MessageCheckResult{
			module:          "quotas",
			suggestedAction: messageTempFail,
			message:         rejectMsg,
			score:           100,
			determinants:    determinants,
		}
	}

	for quota_id := range quotas {
		StatsCounters["RdbmsQueries"].increase(1)
		_, err := QuotaInsertQuotaMessageStmt.Exec(quota_id, msg.QueueId)
		if err != nil {
			panic("Could not execute QuotaInsertQuotaMessageStmt in quotasIsAllowed(). Error: " + err.Error())
		}
	}

	return &MessageCheckResult{
		module:          "quotas",
		suggestedAction: messagePermit,
		message:         "",
		score:           1,
		determinants:    determinants,
	}
}

func quotasGetCounts(msg *Message, applyRegexes bool) ([]*quotasSelectResultSet, error) {
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

func quotasGetCountsRaw(msg *Message) (*sql.Rows, error) {
	sess := *msg.session
	factors := quotasGetMsgFactors(msg)

	StatsCounters["RdbmsQueries"].increase(1)
	_, hasFactorRecipient := factors["recipient"]
	if len(msg.Rcpt) == 1 || !hasFactorRecipient {
		return QuotasSelectStmt.Query(
			msg.QueueId,
			msg.From,
			msg.Rcpt[0],
			sess.getIp(),
			sess.getSaslUsername(),
		)
	}

	factorValueCounts := map[string]int{
		"recipient": len(msg.Rcpt),
	}

	query := quotasGetSelectQuery(factorValueCounts)
	queryArgs := make([]interface{}, 4+len(msg.Rcpt))
	queryArgs[0] = interface{}(msg.QueueId)
	queryArgs[1] = interface{}(msg.From)
	i := 2
	for i = i; i < len(msg.Rcpt)+2; i++ {
		queryArgs[i] = interface{}(msg.Rcpt[i-2])
	}
	queryArgs[i] = interface{}(sess.getIp())
	queryArgs[i+1] = interface{}(sess.getSaslUsername())

	return Rdbms.Query(query, queryArgs...)
}

func quotasGetRegexCounts(msg *Message, factor string, factorValues []string) []*quotasSelectResultSet {

	totalRowCount := int64(0)
	for _, factorValue := range factorValues {
		if factorValue == "" {
			continue
		}

		StatsCounters["RdbmsQueries"].increase(1)
		res, err := QuotaInsertDeducedQuotaStmt.Exec(factorValue, factor, factorValue)
		if err != nil {
			panic("Could not execute QuotaInsertDeducedQuotaStmt in quotasGetRegexCounts(). Error: " + err.Error())
		}
		rowCnt, err := res.RowsAffected()
		if err != nil {
			panic(
				"Could not get rowsAffected from QuotaInsertDeducedQuotaStmt in quotasGetRegexCounts(). Error: " +
					err.Error())
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
			LEFT JOIN quota_class c           ON c.id = p.class
			LEFT JOIN quota_profile_period pp ON p.id = pp.profile
			LEFT JOIN quota_message	qm        ON qm.quota = q.id AND qm.message != ?
			LEFT JOIN message m               ON m.id = qm.message AND (m.verdict = 'permit' OR m.verdict IS NULL)
				AND m.date > FROM_UNIXTIME(UNIX_TIMESTAMP() - %d - pp.period)
		WHERE (`+strings.Join(pieces, " OR ")+`) AND q.is_regex = 0 AND c.cluegetter_instance = %d
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

func quotasGetMsgFactors(msg *Message) map[string][]string {
	sess := *msg.session
	factors := make(map[string][]string)

	if Config.Quotas.Account_Sender {
		factors["sender"] = []string{msg.From}
	}
	if Config.Quotas.Account_Recipient {
		rcpts := make([]string, len(msg.Rcpt))
		copy(rcpts, msg.Rcpt)
		factors["recipient"] = rcpts
	}
	if Config.Quotas.Account_Client_Address {
		factors["client_address"] = []string{sess.getIp()}
	}
	if Config.Quotas.Account_Sasl_Username {
		factors["sasl_username"] = []string{sess.getSaslUsername()}
	}

	return factors
}
