// GlueGetter - Does things with mail
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
	"log"
	"strings"
	"strconv"
)

type quotasSelectResultSet struct {
	id       uint64
	selector string
	period   uint32
	curb     uint32
	count    uint32
}

var QuoatasSelectStmt = *new(*sql.Stmt)
var QuoataInsertTrackingStmt = *new(*sql.Stmt)

func quotasStart(c chan int) {
	stmt, err := Rdbms.Prepare(getSelectQuery())
	if err != nil {
		log.Fatal(err)
	}
	QuoatasSelectStmt = stmt

	stmt, err = Rdbms.Prepare("INSERT INTO quota_tracking (id, date,quota, count) VALUES (null, NOW(), ?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	QuoataInsertTrackingStmt = stmt

	log.Println(fmt.Sprintf("Quotas module started successfully"))
	c <- 1 // Let parent know we've connected successfully
	<-c
	stmt.Close()
	log.Println(fmt.Sprintf("Quotas module ended"))
	c <- 1
}

func quotasIsAllowed(policyRequest map[string]string) string {
	counts := quotasGetCounts(policyRequest)
	quotas := make(map[uint64]struct{})

	policy_count, _ := strconv.ParseUint(policyRequest["count"], 10, 32)
	for _, row := range counts {
		quotas[row.id] = struct{}{}
		if (row.count + uint32(policy_count) ) > row.curb {
			return fmt.Sprintf("REJECT Policy reject; Exceeding quota, max of %d messages in last %d seconds for %s '%s'",
				row.curb, row.period, row.selector, policyRequest[row.selector])
		}
	}

	for quota_id, _ := range quotas {
		_, err := QuoataInsertTrackingStmt.Exec(quota_id, policy_count)
		if err != nil {
			log.Fatal(err) // TODO
		}
	}

	return "DUNNO"
}

func quotasGetCounts(policyRequest map[string]string) []*quotasSelectResultSet {
	results := []*quotasSelectResultSet{}

	rows, err := QuoatasSelectStmt.Query(
		policyRequest["sender"],
		policyRequest["recipient"],
		policyRequest["client_address"],
		policyRequest["sasl_username"],
	)
	if err != nil {
		log.Fatal(err) // TODO
	}
	defer rows.Close()
	for rows.Next() {
		r := new(quotasSelectResultSet)
		if err := rows.Scan(&r.id, &r.selector, &r.period, &r.curb, &r.count); err != nil {
			log.Fatal(err) // TODO
		}
		fmt.Println(r)
		results = append(results, r)
	}
	if err = rows.Err(); err != nil {
		log.Fatal(err)
	}

	return results
}

func getSelectQuery() string {
	pieces := []string{"(? IS NULL)", "(? IS NULL)", "(? IS NULL)", "(? IS NULL)"}
	if Config.Quotas.Account_Sender {
		pieces[0] = "(q.selector = 'sender'  AND q.value = ?)"
	}
	if Config.Quotas.Account_Recipient {
		pieces[1] = "(q.selector = 'recipient' AND q.value = ?)"
	}
	if Config.Quotas.Account_Client_Address {
		pieces[2] = "(q.selector = 'client_address' AND q.value = ?)"
	}
	if Config.Quotas.Account_Sasl_Username {
		pieces[3] = "(q.selector = 'sasl_username' AND q.value = ?)"
	}

	if len(pieces) == 0 {
		log.Fatalln("Quotas: No variables were given to account for.")
	}

	return `
		SELECT q.id, q.selector, pp.period, pp.curb, coalesce(sum(t.count), 0) count FROM quota q
			LEFT JOIN quota_profile p         ON p.id = q.profile
			LEFT JOIN quota_profile_period pp ON p.id = pp.profile
			LEFT JOIN quota_tracking t        ON q.id = t.quota AND t.date > FROM_UNIXTIME(UNIX_TIMESTAMP() - pp.period)
		WHERE (` + strings.Join(pieces, " OR ") + ") AND q.is_regex = 0 GROUP BY pp.id, q.id"
}
