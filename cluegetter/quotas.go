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
	"log"
	"strconv"
	"strings"
)

type quotasSelectResultSet struct {
	id       uint64
	selector string
	period   uint32
	curb     uint32
	count    uint32
}

var QuoatasSelectStmt = *new(*sql.Stmt)
var QuoataInsertQuotaMessageStmt = *new(*sql.Stmt)
var QuoataInsertMessageStmt = *new(*sql.Stmt)
var QuoataInsertDeducedQuotaStmt = *new(*sql.Stmt)

func quotasStart(c chan int) {
	stmt, err := Rdbms.Prepare(quotasGetSelectQuery())
	if err != nil {
		log.Fatal(err)
	}
	QuoatasSelectStmt = stmt

	stmt, err = Rdbms.Prepare(
		"INSERT INTO quota_message (quota, message) VALUES (?, ?) ON DUPLICATE KEY UPDATE message=message")
	if err != nil {
		log.Fatal(err)
	}
	QuoataInsertQuotaMessageStmt = stmt

	stmt, err = Rdbms.Prepare(
		"INSERT INTO message (id, date, count) VALUES (?, now(), ?) ON DUPLICATE KEY UPDATE count=?")
	if err != nil {
		log.Fatal(err)
	}
	QuoataInsertMessageStmt = stmt

	stmt, err = Rdbms.Prepare(`INSERT INTO quota (selector, value, profile, instigator, date_added)
								SELECT DISTINCT q.selector, ?, q.profile, q.id, NOW() FROM quota q
								WHERE (q.selector = ? AND q.is_regex = 1 AND ? REGEXP q.value)
								ORDER by q.id ASC`)
	if err != nil {
		log.Fatal(err)
	}
	QuoataInsertDeducedQuotaStmt = stmt

	log.Println(fmt.Sprintf("Quotas module started successfully"))
	c <- 1 // Let parent know we've connected successfully
	<-c
	QuoatasSelectStmt.Close()
	QuoataInsertQuotaMessageStmt.Close()
	QuoataInsertMessageStmt.Close()
	QuoataInsertDeducedQuotaStmt.Close()
	log.Println(fmt.Sprintf("Quotas module ended"))
	c <- 1
}

func quotasIsAllowed(policyRequest map[string]string) string {
	if _, ok := policyRequest["instance"]; !ok {
		log.Fatal("No instance value specified") // TODO
	}

	_, err := QuoataInsertMessageStmt.Exec(policyRequest["instance"], policyRequest["count"], policyRequest["count"])
	if err != nil {
		log.Fatal(err) // TODO
	}

	counts := quotasGetCounts(policyRequest)
	quotas := make(map[uint64]struct{})

	policy_count, _ := strconv.ParseUint(policyRequest["count"], 10, 32)
	for _, row := range counts {
		quotas[row.id] = struct{}{}
		if (row.count + uint32(policy_count)) > row.curb {
			return fmt.Sprintf("REJECT Policy reject; Exceeding quota, max of %d messages per %d seconds for %s '%s'",
				row.curb, row.period, row.selector, policyRequest[row.selector])
		}
	}

	fmt.Println(quotas)
	for quota_id := range quotas {
		_, err := QuoataInsertQuotaMessageStmt.Exec(quota_id, policyRequest["instance"])
		if err != nil {
			log.Fatal(err) // TODO
		}
	}

	return "DUNNO"
}

func quotasGetCounts(policyRequest map[string]string) []*quotasSelectResultSet {
	factors := quotasGetFactors()

	rows, err := QuoatasSelectStmt.Query(
		policyRequest["instance"],
		policyRequest["sender"],
		policyRequest["recipient"],
		policyRequest["client_address"],
		policyRequest["sasl_username"],
	)

	if err != nil {
		log.Fatal(err) // TODO
	}
	defer rows.Close()

	results := []*quotasSelectResultSet{}
	for rows.Next() {
		r := new(quotasSelectResultSet)
		if err := rows.Scan(&r.id, &r.selector, &r.period, &r.curb, &r.count); err != nil {
			log.Fatal(err) // TODO
		}
		results = append(results, r)
		if _, ok := factors[r.selector]; ok {
			delete(factors, r.selector)
		}
	}

	if err = rows.Err(); err != nil {
		log.Fatal(err)
	}

	if len(factors) > 0 {
		res := quotasGetRegexCounts(policyRequest, factors)
		if res != nil {
			results = append(results, res...)
		}
	}

	return results
}

func quotasGetRegexCounts(policyRequest map[string]string, factors map[string]struct{}) []*quotasSelectResultSet {
	// TODO?
	// If there's multiple factors, for which one or more no regex exist, semi-infinite recursion may occur. Yolo

	totalRowCount := int64(0)
	for factor := range factors {
		res, err := QuoataInsertDeducedQuotaStmt.Exec(policyRequest[factor], factor, policyRequest[factor])
		if err != nil {
			log.Fatal(err) // TODO
		}
		rowCnt, err := res.RowsAffected()
		if err != nil {
			log.Fatal(err)
		}
		totalRowCount = +rowCnt
		delete(factors, factor)
	}

	if totalRowCount > 0 {
		return quotasGetCounts(policyRequest)
	}
	return nil
}

func quotasGetSelectQuery() string {
	pieces := []string{"(? IS NULL)", "(? IS NULL)", "(? IS NULL)", "(? IS NULL)"}
	factors := quotasGetFactors()
	index := map[string]int{
		"sender":         0,
		"recipient":      1,
		"client_address": 2,
		"sasl_username":  3,
	}

	for factor := range factors {
		pieces[index[factor]] = "(q.selector = '" + factor + "'  AND q.value = ?)"
	}

	if len(factors) == 0 {
		log.Fatalln("Quotas: No factors were given to account for.")
	}

	sql := `
		SELECT q.id, q.selector, pp.period, pp.curb, coalesce(sum(m.count), 0) count FROM quota q
			LEFT JOIN quota_profile p         ON p.id = q.profile
			LEFT JOIN quota_profile_period pp ON p.id = pp.profile
			LEFT JOIN quota_message	qm        ON qm.quota = q.id
			LEFT JOIN message m               ON m.id = qm.message AND m.date > FROM_UNIXTIME(UNIX_TIMESTAMP() - pp.period)
		WHERE m.id != ? AND (` + strings.Join(pieces, " OR ") + ") AND q.is_regex = 0 GROUP BY pp.id, q.id"
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
