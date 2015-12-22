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
	"github.com/glenn-brown/golang-pkg-pcre/src/pkg/pcre"
	redis "gopkg.in/redis.v3"
	"strconv"
	"strings"
	"sync"
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

type quotasResult struct {
	Curb             uint64
	ExtraCount       uint64
	FactorValue      *string
	FutureTotalCount uint64
	Period           uint64
	Selector         *string
	QuotaKey         *string
}

type quotasRegex struct {
	selector *string
	regex    *pcre.Regexp
	period   int
	curb     int
}

var QuotasSelectStmt = *new(*sql.Stmt)
var QuotaInsertQuotaMessageStmt = *new(*sql.Stmt)
var QuotaInsertDeducedQuotaStmt = *new(*sql.Stmt)
var QuotaGetAllQuotasStmt = *new(*sql.Stmt)
var QuotaGetAllRegexesStmt = *new(*sql.Stmt)

var quotasRegexes *[]*quotasRegex
var quotasRegexesLock *sync.RWMutex

func init() {
	init := quotasStart
	stop := quotasStop
	milterCheck := quotasIsAllowed

	ModuleRegister(&module{
		name:        "quotas",
		init:        &init,
		stop:        &stop,
		milterCheck: &milterCheck,
	})
}

func quotasStart() {
	if Config.Quotas.Enabled != true {
		Log.Info("Skipping Quota module because it was not enabled in the config")
		return
	}

	quotasPrepStmt()
	if Config.Redis.Enabled {
		quotasRedisStart()
		quotasRegexesStart()
	}

	Log.Info("Quotas module started successfully")
}

func quotasRegexesStart() {
	quotasRegexesLock = &sync.RWMutex{}

	go func() {
		ticker := time.NewTicker(time.Duration(3) * time.Minute)
		for {
			select {
			case <-ticker.C:
				quotasRegexesLoad()
			}
		}
	}()

	go quotasRegexesLoad()
}

func quotasRegexesLoad() {
	defer func() {
		if Config.ClueGetter.Exit_On_Panic {
			return
		}
		r := recover()
		if r == nil {
			return
		}
		Log.Error("Panic caught in quotasRegexesLoad(). Recovering. Error: %s", r)
	}()

	Log.Info("Importing regexes from RDBMS")
	t0 := time.Now()

	regexes, err := QuotaGetAllRegexesStmt.Query()
	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		panic("Error occurred while retrieving quotas")
	}

	defer regexes.Close()

	var regexCollection []*quotasRegex
	i := 0
	for regexes.Next() {
		var selector string // sasl_username
		var regexStr string // ^.*$
		var period int      // 86400
		var curb int        // 5000
		regexes.Scan(&selector, &regexStr, &period, &curb)

		regex, err := pcre.Compile(regexStr, 0)
		if err != nil {
			Log.Error("Could not compile regex: /%s/. Ignoring. Error: %s", regexStr, err.String())
			continue
		}

		regexCollection = append(regexCollection, &quotasRegex{
			selector: &selector,
			regex:    &regex,
			period:   period,
			curb:     curb,
		})
		i++
	}

	quotasRegexesLock.Lock()
	defer quotasRegexesLock.Unlock()

	quotasRegexes = &regexCollection
	Log.Info("Imported %d regexes in %.2f seconds", i, time.Now().Sub(t0).Seconds())
}

func quotasRedisStart() {
	go func() {
		ticker := time.NewTicker(time.Duration(5) * time.Minute)
		for {
			select {
			case <-ticker.C:
				quotasRedisUpdateFromRdbms()
			}
		}
	}()

	go quotasRedisUpdateFromRdbms()
}

func quotasRedisUpdateFromRdbms() {
	defer func() {
		if Config.ClueGetter.Exit_On_Panic {
			return
		}
		r := recover()
		if r == nil {
			return
		}
		Log.Error("Panic caught in quotasRedisUpdateFromRdbms(). Recovering. Error: %s", r)
	}()

	Log.Info("Importing quotas into Redis")
	t0 := time.Now()

	quotas, err := QuotaGetAllQuotasStmt.Query()
	if err != nil {
		StatsCounters["RdbmsErrors"].increase(1)
		panic("Error occurred while retrieving quotas")
	}

	groupedQuotas := make(map[string][]string, 0)
	i := 0
	defer quotas.Close()
	for quotas.Next() {
		var selector string // sasl_username
		var value string    // foobar@example.com
		var period int      // 86400
		var curb int        // 5000
		quotas.Scan(&selector, &value, &period, &curb)

		lval := fmt.Sprintf("%d_%d", period, curb)
		if groupedQuota, ok := groupedQuotas[selector+"_"+value]; ok {
			groupedQuota = append(groupedQuota, lval)
		} else {
			groupedQuotas[selector+"_"+value] = []string{lval}
		}

		i++
	}

	// Todo: Use some sort of scripting or pipelining here?
	for k, v := range groupedQuotas {
		key := fmt.Sprintf("{cluegetter-%d-quotas-%s}-definitions", instance, k)
		redisClient.Del(key)
		redisClient.LPush(key, v...)
		redisClient.Expire(key, time.Duration(24)*time.Hour)
	}

	Log.Info("Imported %d quota tuples into Redis in %.2f seconds", i, time.Now().Sub(t0).Seconds())
}

func quotasPrepStmt() {
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

	stmt, err = Rdbms.Prepare(fmt.Sprintf(`
		SELECT q.selector, q.value factorValue, pp.period, pp.curb
			FROM quota q
				LEFT JOIN quota_profile p         ON p.id = q.profile
				LEFT JOIN quota_class c           ON c.id = p.class
				LEFT JOIN quota_profile_period pp ON p.id = pp.profile
			WHERE q.is_regex = 0 AND c.cluegetter_instance = %d
			GROUP BY pp.id, q.id`, instance))
	if err != nil {
		Log.Fatal(err)
	}
	QuotaGetAllQuotasStmt = stmt

	stmt, err = Rdbms.Prepare(fmt.Sprintf(`
		SELECT q.selector, q.value factorValue, pp.period, pp.curb
			FROM quota q
				LEFT JOIN quota_profile p         ON p.id = q.profile
				LEFT JOIN quota_class c           ON c.id = p.class
				LEFT JOIN quota_profile_period pp ON p.id = pp.profile
			WHERE q.is_regex = 1 AND c.cluegetter_instance = %d
			GROUP BY pp.id, q.id`, instance))
	if err != nil {
		Log.Fatal(err)
	}
	QuotaGetAllRegexesStmt = stmt
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
	if !Config.Quotas.Enabled {
		return nil
	}

	if Config.Redis.Enabled {
		return quotasRedisIsAllowed(msg)
	}

	return quotasRdbmsIsAllowed(msg)
}

func quotasRedisIsAllowed(msg *Message) *MessageCheckResult {
	c := make(chan *quotasResult)
	var wg sync.WaitGroup

	callbacks := make([]*func(*Message, int), 0)
	for selector, selectorValues := range quotasGetMsgFactors(msg) {
		var extra_count int

		if selector != "recipient" {
			extra_count = len(msg.Rcpt)
		} else {
			extra_count = int(1)
		}

		for _, selectorValue := range selectorValues {
			lselector := selector
			lselectorValue := selectorValue
			wg.Add(1)
			go func() {
				quotasRedisPollQuotasBySelector(c, &lselector, &lselectorValue, extra_count)
				wg.Done()
			}()
		}
	}

	go func() {
		wg.Wait()
		close(c)
	}()

	results := make([]*quotasResult, 0)
	for result := range c {
		results = append(results, result)

		for i := 1; i <= int(result.ExtraCount); i++ {
			callback := quotasRedisAddMsg(result.QuotaKey, i)
			callbacks = append(callbacks, &callback)
		}
	}

	determinants := map[string]interface{}{"quotas": results}

	rejectMsg := ""
	for _, result := range results {
		if result.FutureTotalCount > result.Curb {
			Log.Notice("Quota Exceeding, max of %d messages per %d seconds for %s '%s'",
				result.Curb, result.Period, *result.Selector, *result.FactorValue)
			rejectMsg = fmt.Sprintf("REJECT Policy reject; Exceeding quota, max of %d messages per %d seconds for %s '%s'",
				result.Curb, result.Period, *result.Selector, *result.FactorValue)
		} else {
			Log.Info("Quota Updated, Adding %d message(s) to total of %d (max %d) for last %d seconds for %s '%s'",
				result.ExtraCount, result.FutureTotalCount, result.Curb, result.Period, *result.Selector, *result.FactorValue)
		}
	}

	if rejectMsg != "" {
		return &MessageCheckResult{
			module:          "quotas",
			suggestedAction: messageTempFail,
			message:         rejectMsg,
			score:           100,
			determinants:    determinants,
			callbacks:       callbacks,
		}
	}

	return &MessageCheckResult{
		module:          "quotas",
		suggestedAction: messagePermit,
		message:         "",
		score:           1,
		determinants:    determinants,
		callbacks:       callbacks,
	}
}

func quotasRedisPollQuotasBySelector(c chan *quotasResult, selector, selectorValue *string, extra_count int) {
	key := fmt.Sprintf("{cluegetter-%d-quotas-%s_%s}-definitions", instance, *selector, *selectorValue)

	var wg sync.WaitGroup
	i := 0
	for _, quota := range redisClient.LRange(key, 0, -1).Val() {
		wg.Add(1)
		i++
		go func(quota string) {
			quotasRedisPollQuotasBySelectorAndPeriod(c, quota, selector, selectorValue, extra_count)
			wg.Done()
		}(quota)
	}

	wg.Wait()

	if i != 0 {
		return
	}

	if !quotasRedisInsertRegexesForSelector(selector, selectorValue) {
		return
	}

	for _, quota := range redisClient.LRange(key, 0, -1).Val() {
		wg.Add(1)
		lquota := quota
		go func() {
			quotasRedisPollQuotasBySelectorAndPeriod(c, lquota, selector, selectorValue, extra_count)
			wg.Done()
		}()
	}

	wg.Wait()
}

func quotasRedisInsertRegexesForSelector(selector, selectorValue *string) bool {
	quotasRegexesLock.RLock()
	regexes := *quotasRegexes
	quotasRegexesLock.RUnlock()

	inserted := 0
	for _, regex := range regexes {
		if *regex.selector != *selector {
			continue
		}

		regexp := *regex.regex
		if len(regexp.FindIndex([]byte(*selectorValue), 0)) == 0 {
			continue
		}

		key := fmt.Sprintf("{cluegetter-%d-quotas-%s_%s}-definitions", instance, *selector, *selectorValue)
		redisClient.LPush(key, fmt.Sprintf("%d_%d", regex.period, regex.curb))
		redisClient.Expire(key, time.Duration(24)*time.Hour)
		inserted++
	}

	return inserted != 0
}

func quotasRedisPollQuotasBySelectorAndPeriod(c chan *quotasResult, quota string,
	selector, selectorValue *string, extra_count int) {
	now := time.Now().Unix()
	period, _ := strconv.Atoi(strings.Split(quota, "_")[0])
	curb, _ := strconv.Atoi(strings.Split(quota, "_")[1])
	startPeriod := fmt.Sprintf("%d", now-int64(period))

	quotaKey := fmt.Sprintf("{cluegetter-%d-quotas-%s_%s}-counts-%d", instance, *selector, *selectorValue, period)
	count := redisClient.ZCount(quotaKey, startPeriod, fmt.Sprintf("%d", now)).Val()
	if (int(count) + extra_count) > curb {
		redisClient.ZRemRangeByScore(quotaKey, "-inf", startPeriod)
		count = redisClient.ZCount(quotaKey, startPeriod, fmt.Sprintf("%d", now)).Val()
	}

	c <- &quotasResult{
		Curb:             uint64(curb),
		ExtraCount:       uint64(extra_count),
		FactorValue:      selectorValue,
		FutureTotalCount: uint64(int(count) + extra_count),
		Period:           uint64(period),
		Selector:         selector,
		QuotaKey:         &quotaKey,
	}
}

func quotasRedisAddMsg(quotasKey *string, count int) func(*Message, int) {
	redisKey := quotasKey
	localCount := count

	return func(msg *Message, verdict int) {
		if !Config.Redis.Enabled {
			return
		}

		if verdict != 0 {
			return
		}

		now := time.Now().Unix()
		redisClient.ZAdd(*redisKey, redis.Z{float64(now), msg.QueueId + "-" + strconv.Itoa(localCount)})
		redisClient.Expire(*redisKey, 24*time.Hour)
	}
}

func quotasRdbmsAddMsg(quota_id uint64) func(*Message, int) {
	return func(msg *Message, verdict int) {
		if verdict != 0 {
			return
		}

		// Right now we have no sync mechanism that ensures that the quotas
		// are persisted after the message (the message being persisted
		// asynchronously. As such, we use a crude delay and hope
		// for the best. Improvements are welcome.
		time.Sleep(5 * time.Second)

		StatsCounters["RdbmsQueries"].increase(1)
		_, err := QuotaInsertQuotaMessageStmt.Exec(quota_id, msg.QueueId)
		if err != nil {
			panic("Could not execute QuotaInsertQuotaMessageStmt in quotasRdbmsAddMsg(). Error: " + err.Error())
		}
	}
}

func quotasRdbmsIsAllowed(msg *Message) *MessageCheckResult {
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

	callbacks := make([]*func(*Message, int), 0)
	for quota_id := range quotas {
		callback := quotasRdbmsAddMsg(quota_id)
		callbacks = append(callbacks, &callback)
	}

	return &MessageCheckResult{
		module:          "quotas",
		suggestedAction: messagePermit,
		message:         "",
		score:           1,
		determinants:    determinants,
		callbacks:       callbacks,
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
