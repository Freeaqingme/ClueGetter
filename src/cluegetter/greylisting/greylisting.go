// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package greylisting

import (
	"database/sql"
	"fmt"
	"net"
	"time"

	"cluegetter/core"

	libspf2 "github.com/Freeaqingme/go-libspf2"
)

const ModuleName = "greylisting"

// A period of a month seems legit. And then we want to allow for cases
// like a news letter sent every first Monday of the month
const greylist_validity = 40

var greylistUpdateWhitelistStmt = *new(*sql.Stmt)
var greylistGetWhitelist = *new(*sql.Stmt)
var greylistSpf2 = libspf2.NewClient()

type greylistVerdict struct {
	verdict string
	date    *time.Time
}

type module struct {
	*core.BaseModule

	cg *core.Cluegetter
}

func init() {
	core.ModuleRegister(&module{})
}

func (m *module) Name() string {
	return ModuleName
}

func (m *module) SetCluegetter(cg *core.Cluegetter) {
	m.cg = cg
}

func (m *module) Enable() bool {
	return m.cg.Config.Greylisting.Enabled
}

func (m *module) Init() {
	m.prepStmt()
	go func() {
		ticker := time.NewTicker(time.Duration(1) * time.Minute)
		for {
			select {
			case <-ticker.C:
				m.updateWhitelist()
			}
		}
	}()

	go m.updateWhitelist()
}

func (m *module) prepStmt() {
	_, tzOffset := time.Now().Local().Zone()

	stmt, err := m.cg.Rdbms().Prepare(fmt.Sprintf(`
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
	`, m.cg.Instance(), tzOffset))

	greylistUpdateWhitelistStmt = stmt

	stmt, err = m.cg.Rdbms().Prepare(fmt.Sprintf(`
		SELECT ip, UNIX_TIMESTAMP(last_seen) - ? ttl FROM greylist_whitelist
		WHERE cluegetter_instance = %d
			AND last_seen > (DATE_SUB(CURDATE(), INTERVAL %d DAY))
	`, m.cg.Instance(), greylist_validity))
	if err != nil {
		m.cg.Log.Fatalf("%s", err)
	}

	greylistGetWhitelist = stmt
}

func (m *module) updateWhitelist() {
	core.CluegetterRecover("greylist.updateWhitelist")

	key := fmt.Sprintf("cluegetter-%d-greylisting-schedule-greylistUpdateWhitelist", m.cg.Instance())
	set, err := m.cg.Redis.SetNX(key, m.cg.Hostname(), 5*time.Minute).Result()
	if err != nil {
		m.cg.Log.Errorf("Could not update greylist whitelist schedule: %s", err.Error())
	} else if !set {
		m.cg.Log.Debugf("Greylist whitelist update was run recently. Sipping")
		return
	}

	t0 := time.Now()
	res, err := greylistUpdateWhitelistStmt.Exec()
	if err != nil {
		m.cg.Log.Errorf("Could not update greylist whitelist: %s", err.Error())
	}

	rowCnt, err := res.RowsAffected()
	if err != nil {
		m.cg.Log.Errorf("Error while fetching number of affected rows: ", err)
		return
	}

	m.cg.Log.Infof("Updated RDBMS greylist whitelist with %d to %d entries in %s",
		int(rowCnt/2), rowCnt, time.Now().Sub(t0).String())

	m.populateRedis()
}

func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	if !msg.Session().Config().Greylisting.Enabled {
		return nil
	}

	ip := msg.Session().Ip

	whitelist := msg.Session().Config().Greylisting.Whitelist_Spf
	res, spfDomain, spfWhitelistErr := m.ipIsSpfWhitelisted(net.ParseIP(ip), done, whitelist)
	if res {
		m.cg.Log.Debugf("Found %s in %s SPF record", ip, spfDomain)
		return &core.MessageCheckResult{
			Module:          ModuleName,
			SuggestedAction: core.MessagePermit,
			Message:         "",
			Score:           1,
			Determinants: map[string]interface{}{
				"Found in SPF whitelist": "true",
				"SpfError":               spfWhitelistErr,
				"SpfDomain":              spfDomain,
			},
		}
	}

	if m.ipIsWhitelisted(&ip) {
		m.cg.Log.Debugf("Found %s in greylist whitelist", ip)
		return &core.MessageCheckResult{
			Module:          ModuleName,
			SuggestedAction: core.MessagePermit,
			Message:         "",
			Score:           1,
			Determinants: map[string]interface{}{
				"Found in whitelist":     "true",
				"Found in SPF whitelist": "false",
				"SpfError":               spfWhitelistErr,
				"SpfDomain":              spfDomain,
			},
		}
	}

	return m.getVerdict(msg, spfWhitelistErr, spfDomain)
}

func (m *module) getVerdict(msg *core.Message, spfWhitelistErr error, spfDomain string) *core.MessageCheckResult {
	determinants := map[string]interface{}{
		"Found in whitelist":     "false",
		"Found in SPF whitelist": "false",
		"SpfError":               spfWhitelistErr,
		"SpfDomain":              spfDomain,
		"Store":                  "redis",
	}

	sess := msg.Session()
	key := fmt.Sprintf("cluegetter-%d-greylisting-msg-%s_%s_%s", m.cg.Instance(), sess.Ip, msg.From, msg.Rcpt)
	res, err := m.cg.Redis.Get(key).Int64()
	if err == nil {
		determinants["time_diff"] = time.Now().Unix() - res
		if (res + (int64(sess.Config().Greylisting.Initial_Period) * 60)) < time.Now().Unix() {
			return &core.MessageCheckResult{
				Module:          ModuleName,
				SuggestedAction: core.MessagePermit,
				Message:         "",
				Score:           1,
				Determinants:    determinants,
			}
		}
	} else {
		m.cg.Redis.Set(key, time.Now().Unix(), time.Duration(90)*time.Minute)
	}

	return &core.MessageCheckResult{
		Module:          ModuleName,
		SuggestedAction: core.MessageTempFail,
		Message:         "Greylisting in effect, please come back later",
		Score:           sess.Config().Greylisting.Initial_Score,
		Determinants:    determinants,
	}
}

func (m *module) ipIsWhitelisted(ip *string) bool {
	key := fmt.Sprintf("cluegetter-%d-greylisting-ip-%s", m.cg.Instance(), *ip)
	return m.cg.Redis.Exists(key).Val()
}

func (m *module) ipIsSpfWhitelisted(ip net.IP, done chan bool, whitelist []string) (bool, string, error) {
	var error error
	for _, whitelistDomain := range whitelist {
		res, err := greylistSpf2.Query(whitelistDomain, ip)
		if err != nil {
			error = err
			m.cg.Log.Errorf("Error while retrieving SPF for %s from %s: %s", ip, whitelistDomain, err)
			continue
		}

		m.cg.Log.Debugf("Got SPF result for %s from %s: %s", ip, whitelistDomain, res)
		if res == libspf2.SPFResultPASS {
			return true, whitelistDomain, error
		}
	}

	return false, "", error
}

func (m *module) populateRedis() {
	core.CluegetterRecover("greylist.populateRedis")

	m.cg.Log.Infof("Importing greylist whitelist into Redis")

	t0 := time.Now()
	startDate := time.Now().Unix() - (greylist_validity * 86400)

	whitelist, err := greylistGetWhitelist.Query(startDate)
	if err != nil {
		panic("Error occurred while retrieving whitelist")
	}

	i := 0
	defer whitelist.Close()
	for whitelist.Next() {
		var ip string
		var ttl uint64
		whitelist.Scan(&ip, &ttl)

		key := fmt.Sprintf("cluegetter-%d-greylisting-ip-%s", m.cg.Instance(), ip)
		m.cg.Redis.Set(key, "", time.Duration(ttl)*time.Second)
		i++
	}

	m.cg.Log.Infof("Imported %d greylist whitelist items into Redis in %.2f seconds", i, time.Now().Sub(t0).Seconds())
}
