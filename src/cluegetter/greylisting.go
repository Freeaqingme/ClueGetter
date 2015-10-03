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
	libspf2 "github.com/Freeaqingme/go-libspf2"
	"net"
	"strings"
	"time"
)

var greylistGetRecentVerdictsStmt = *new(*sql.Stmt)
var greylistSelectFromWhitelist = *new(*sql.Stmt)
var greylistUpdateWhitelistStmt = *new(*sql.Stmt)
var greylistSpf2 = libspf2.NewClient()

type greylistVerdict struct {
	verdict string
	date    *time.Time
}

func greylistStart() {
	if Config.Greylisting.Enabled != true {
		Log.Info("Skipping Greylist module because it was not enabled in the config")
		return
	}

	greylistPrepStmt()
	go greylistUpdateWhitelist()

	Log.Info("Greylist module started")
}

func greylistPrepStmt() {
	_, tzOffset := time.Now().Local().Zone()
	stmt, err := Rdbms.Prepare(fmt.Sprintf(`
		SELECT m.verdict, m.date FROM message m
			LEFT JOIN message_recipient mr ON mr.message = m.id
			LEFT JOIN recipient r ON mr.recipient = r.id
			LEFT JOIN session s ON s.id = m.session
		WHERE (m.sender_local = ? AND m.sender_domain = ?)
			AND (r.local = ? AND r.domain = ?)
			AND s.ip = ?
			AND s.cluegetter_instance = %d
			AND m.date > FROM_UNIXTIME(UNIX_TIMESTAMP() - %d - 86400)
			AND m.verdict IS NOT NULL
		ORDER BY m.date ASC
	`, instance, tzOffset)) // sender_local, sender_domain, rcpt_local, rcpt_domain, ip
	if err != nil {
		Log.Fatal(err)
	}

	greylistGetRecentVerdictsStmt = stmt

	stmt, err = Rdbms.Prepare(fmt.Sprintf(`
		INSERT INTO greylist_whitelist (cluegetter_instance, ip, last_seen)
		SELECT s.cluegetter_instance, s.ip, MAX(m.date)
			FROM message m
				LEFT JOIN message_recipient mr ON mr.message = m.id
				LEFT JOIN recipient r ON mr.recipient = r.id
				LEFT JOIN session s ON s.id = m.session
			 WHERE s.cluegetter_instance = %d
				AND m.date > FROM_UNIXTIME(UNIX_TIMESTAMP() - %d - 86400)
				AND m.verdict = 'permit'
			GROUP BY s.cluegetter_instance, s.ip
		ON DUPLICATE KEY UPDATE last_seen = VALUES(last_seen)
	`, instance, tzOffset))

	greylistUpdateWhitelistStmt = stmt

	stmt, err = Rdbms.Prepare(fmt.Sprintf(`
		SELECT 1 FROM greylist_whitelist
		WHERE cluegetter_instance = %d
			AND	ip = ?
		LIMIT 0,1
	`, instance))
	if err != nil {
		Log.Fatal(err)
	}

	greylistSelectFromWhitelist = stmt
}

func greylistUpdateWhitelist() {
	ticker := time.NewTicker(300 * time.Second)

	for {
		select {
		case <-ticker.C:
			t0 := time.Now()
			res, err := greylistUpdateWhitelistStmt.Exec()
			if err != nil {
				Log.Error("Could not update greylist whitelist: %s", err.Error())
			}

			rowCnt, err := res.RowsAffected()
			if err != nil {
				Log.Error("Error while fetching number of affected rows: ", err)
				continue
			}

			Log.Info("Updated greylist whitelist with %d to %d entries in %s",
				int(rowCnt/2), rowCnt, time.Now().Sub(t0).String())
		}
	}
}

func greylistGetResult(msg Message, done chan bool) *MessageCheckResult {
	ip := (*msg.getSession()).getIp()

	res, spfDomain, spfWhitelistErr := greylistIsSpfWhitelisted(net.ParseIP(ip), done)
	if res {
		Log.Debug("Found %s in %s SPF record", ip, spfDomain)
		return &MessageCheckResult{
			module:          "greylisting",
			suggestedAction: messagePermit,
			message:         "",
			score:           1,
			determinants: map[string]interface{}{
				"Found in SPF whitelist": "true",
				"SpfError":               spfWhitelistErr,
				"SpfDomain":              spfDomain,
			},
		}
	}

	StatsCounters["RdbmsQueries"].increase(1)
	whitelistRows, err := greylistSelectFromWhitelist.Query(ip)

	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		Log.Error("Error occurred while retrieving from whitelist: %s", err.Error())
	} else {
		defer whitelistRows.Close()
		for whitelistRows.Next() {
			Log.Debug("Found %s in greylist whitelist", ip)
			return &MessageCheckResult{
				module:          "greylisting",
				suggestedAction: messagePermit,
				message:         "",
				score:           1,
				determinants: map[string]interface{}{
					"Found in whitelist":     "true",
					"Found in SPF whitelist": "false",
					"SpfError":               spfWhitelistErr,
					"SpfDomain":              spfDomain,
				},
			}
		}
	}

	verdicts := greylistGetRecentVerdicts(msg)
	allowCount := 0
	disallowCount := 0
	for _, verdict := range *verdicts {
		if verdict.verdict == "permit" {
			allowCount = allowCount + 1
		} else {
			disallowCount = disallowCount + 1
		}
	}

	timeDiff := -1.0
	if allowCount > 0 || disallowCount > 0 {
		firstVerdict := (*verdicts)[0]
		timeDiff = time.Since((*firstVerdict.date)).Minutes()
	}
	determinants := map[string]interface{}{
		"verdicts_allow":         allowCount,
		"verdicts_disallow":      disallowCount,
		"time_diff":              timeDiff,
		"Found in whitelist":     "false",
		"Found in SPF whitelist": "false",
		"SpfError":               spfWhitelistErr,
		"SpfDomain":              spfDomain,
	}

	Log.Debug("%d Got %d allow verdicts, %d disallow verdicts in greylist module. First verdict was %.2f minutes ago",
		(*msg.getSession()).getId(), allowCount, disallowCount, timeDiff)

	if allowCount > 0 || timeDiff > float64(Config.Greylisting.Initial_Period) {
		return &MessageCheckResult{
			module:          "greylisting",
			suggestedAction: messagePermit,
			message:         "",
			score:           1,
			determinants:    determinants,
		}
	}

	return &MessageCheckResult{
		module:          "greylisting",
		suggestedAction: messageTempFail,
		message:         "Greylisting in effect, please come back later",
		score:           Config.Greylisting.Initial_Score,
		determinants:    determinants,
	}
}

func greylistIsSpfWhitelisted(ip net.IP, done chan bool) (bool, string, error) {
	var error error
	for _, whitelistDomain := range Config.Greylisting.Whitelist_Spf {
		res, err := greylistSpf2.Query(whitelistDomain, ip)
		if err != nil {
			error = err
			Log.Error("Error while retrieving SPF for %s from %s: %s", ip, whitelistDomain, err)
			continue
		}

		Log.Debug("Got SPF result for %s from %s: %s", ip, whitelistDomain, res)
		if res == libspf2.SPFResultPASS {
			return true, whitelistDomain, error
		}

		_, allowContinuing := <-done
		if !allowContinuing {
			break
		}
	}

	return false, "", error
}

func greylistGetRecentVerdicts(msg Message) *[]greylistVerdict {
	fromLocal := ""
	fromDomain := ""
	if strings.Index(msg.getFrom(), "@") != -1 {
		fromLocal = strings.Split(msg.getFrom(), "@")[0]
		fromDomain = strings.Split(msg.getFrom(), "@")[1]
	} else {
		fromLocal = msg.getFrom()
	}

	StatsCounters["RdbmsQueries"].increase(1)
	verdictRows, err := greylistGetRecentVerdictsStmt.Query(
		fromLocal,
		fromDomain,
		strings.Split(msg.getRecipients()[0], "@")[0],
		strings.Split(msg.getRecipients()[0], "@")[1],
		(*msg.getSession()).getIp(),
	)

	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		panic("Error occurred while retrieving past verdicts")
	}

	defer verdictRows.Close()
	verdicts := make([]greylistVerdict, 0)
	for verdictRows.Next() {
		verdict := greylistVerdict{}
		verdictRows.Scan(&verdict.verdict, &verdict.date)
		verdicts = append(verdicts, verdict)
	}

	return &verdicts
}
