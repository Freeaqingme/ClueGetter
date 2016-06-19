// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/glenn-brown/golang-pkg-pcre/src/pkg/pcre"
	redis "gopkg.in/redis.v3"
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

var QuotaGetAllQuotasStmt = *new(*sql.Stmt)
var QuotaGetAllRegexesStmt = *new(*sql.Stmt)

var quotasRegexes []*quotasRegex
var quotasRegexesLock *sync.RWMutex

const (
	QUOTA_FACTOR_SENDER           = "sender"
	QUOTA_FACTOR_SENDER_DOMAIN    = "sender_domain"
	QUOTA_FACTOR_RECIPIENT        = "recipient"
	QUOTA_FACTOR_RECIPIENT_DOMAIN = "recipient_domain"
	QUOTA_FACTOR_CLIENT_ADDRESS   = "client_address"
	QUOTA_FACTOR_SASL_USERNAME    = "sasl_username"
)

func init() {
	enable := func() bool { return Config.Quotas.Enabled }
	init := quotasStart
	stop := quotasStop
	milterCheck := quotasIsAllowed

	ModuleRegister(&ModuleOld{
		name:        "quotas",
		enable:      &enable,
		init:        &init,
		stop:        &stop,
		milterCheck: &milterCheck,
		httpHandlers: map[string]HttpCallback{
			"/quotas/sasl_username/": quotasSasluserStats,
		},
	})
}

func quotasStart() {
	quotasPrepStmt()
	if !Config.Redis.Enabled {
		Log.Fatal("Cannot use quotas module without Redis")
	}

	quotasRedisStart()
	quotasRegexesStart()
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

	quotasRegexes = regexCollection
	Log.Info("Imported %d regexes in %.2f seconds", i, time.Now().Sub(t0).Seconds())
}

func quotasRedisStart() {
	go func() {
		ticker := time.NewTicker(time.Duration(1) * time.Minute)
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

	key := fmt.Sprintf("cluegetter-%d-quotas-schedule-quotasRedisUpdateFromRdbms", instance)
	set, err := redisClient.SetNX(key, hostname, 5*time.Minute).Result()
	if err != nil {
		Log.Error("Could not update quotasRedisUpdateFromRdbms schedule: %s", err.Error())
	} else if !set {
		Log.Debug("Update Quotas From Rdbms was run recently. Sipping")
		return
	}

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
		if _, ok := groupedQuotas[selector+"_"+value]; ok {
			groupedQuotas[selector+"_"+value] = append(groupedQuotas[selector+"_"+value], lval)
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
	stmt, err := Rdbms.Prepare(fmt.Sprintf(`
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

	Log.Info("Quotas module ended")
}

func quotasIsAllowed(msg *Message, _ chan bool) *MessageCheckResult {
	if !Config.Quotas.Enabled || !msg.session.config.Quotas.Enabled {
		return nil
	}

	return quotasRedisIsAllowed(msg)
}

func quotasRedisIsAllowed(msg *Message) *MessageCheckResult {
	c := make(chan *quotasResult)
	var wg sync.WaitGroup

	callbacks := make([]*func(*Message, int), 0)
	for selector, selectorValues := range quotasGetMsgFactors(msg) {
		var extra_count int

		if selector != QUOTA_FACTOR_RECIPIENT && selector != QUOTA_FACTOR_RECIPIENT_DOMAIN {
			extra_count = len(msg.Rcpt)
		} else {
			extra_count = int(1)
		}

		for _, selectorValue := range selectorValues {
			lselector := selector
			lselectorValue := selectorValue
			wg.Add(1)
			go func() {
				quotasRedisPollQuotasBySelector(c, &lselector, &lselectorValue, instance, extra_count)
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
			Module:          "quotas",
			SuggestedAction: MessageTempFail,
			Message:         rejectMsg,
			Score:           100,
			Determinants:    determinants,
			Callbacks:       callbacks,
		}
	}

	return &MessageCheckResult{
		Module:          "quotas",
		SuggestedAction: MessagePermit,
		Message:         "",
		Score:           1,
		Determinants:    determinants,
		Callbacks:       callbacks,
	}
}

func quotasRedisPollQuotasBySelector(c chan *quotasResult, selector, selectorValue *string, findInstance uint, extra_count int) {
	key := fmt.Sprintf("{cluegetter-%d-quotas-%s_%s}-definitions", findInstance, *selector, *selectorValue)

	var wg sync.WaitGroup
	i := 0
	for _, quota := range redisClient.LRange(key, 0, -1).Val() {
		wg.Add(1)
		i++
		go func(quota string) {
			quotasRedisPollQuotasBySelectorAndPeriod(c, quota, selector, selectorValue, findInstance, extra_count)
			wg.Done()
		}(quota)
	}

	wg.Wait()

	if i != 0 || findInstance != instance {
		return
	}

	if !quotasRedisInsertRegexesForSelector(selector, selectorValue) {
		return
	}

	for _, quota := range redisClient.LRange(key, 0, -1).Val() {
		wg.Add(1)
		lquota := quota
		go func() {
			quotasRedisPollQuotasBySelectorAndPeriod(c, lquota, selector, selectorValue, instance, extra_count)
			wg.Done()
		}()
	}

	wg.Wait()
}

func quotasRedisInsertRegexesForSelector(selector, selectorValue *string) bool {
	quotasRegexesLock.RLock()
	regexes := quotasRegexes
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
	selector, selectorValue *string, instance uint, extra_count int) {
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

func quotasGetMsgFactors(msg *Message) map[string][]string {
	sess := *msg.session
	factors := make(map[string][]string)

	if Config.Quotas.Account_Sender {
		factors[QUOTA_FACTOR_SENDER] = []string{msg.From.String()}
	}
	if Config.Quotas.Account_Sender_Domain {
		_, domain := messageParseAddress(msg.From.String(), true)
		factors[QUOTA_FACTOR_SENDER_DOMAIN] = []string{domain}
	}
	if Config.Quotas.Account_Recipient {
		rcpts := make([]string, len(msg.Rcpt))
		for k, v := range msg.Rcpt {
			rcpts[k] = v.String()
		}
		factors[QUOTA_FACTOR_RECIPIENT] = rcpts
	}
	if Config.Quotas.Account_Recipient_Domain {
		rcptDomains := make([]string, len(msg.Rcpt))
		for k, v := range msg.Rcpt {
			rcptDomains[k] = v.Domain()
		}
		factors[QUOTA_FACTOR_RECIPIENT_DOMAIN] = rcptDomains
	}
	if Config.Quotas.Account_Client_Address {
		factors[QUOTA_FACTOR_CLIENT_ADDRESS] = []string{sess.getIp()}
	}
	if Config.Quotas.Account_Sasl_Username {
		factors[QUOTA_FACTOR_SASL_USERNAME] = []string{sess.getSaslUsername()}
	}

	return factors
}

func quotasSasluserStats(w http.ResponseWriter, r *http.Request) {
	saslUser := r.URL.Path[len("/quotas/sasl_username/"):]
	if len(saslUser) == 0 {
		http.Error(w, "No sasl username provided", http.StatusNotFound)
		return
	}

	if r.FormValue("instance") == "" {
		q := r.URL.Query()
		q.Set("instance", strconv.FormatUint(uint64(instance), 10))
		r.URL.RawQuery = q.Encode()
		http.Redirect(w, r, r.URL.String(), 301)
		return
	}

	instance, err := strconv.ParseUint(r.FormValue("instance"), 10, 64)
	if err != nil {
		http.Error(w, "Parameter 'instance' could not be parsed", http.StatusBadRequest)
		return
	}

	results := make(chan *quotasResult)
	go func() {
		selector := QUOTA_FACTOR_SASL_USERNAME
		quotasRedisPollQuotasBySelector(results, &selector, &saslUser, uint(instance), 0)
		close(results)
	}()

	jsonData := make([]interface{}, 0)
	for result := range results {
		jsonData = append(jsonData, struct {
			Curb         uint64
			CurrentCount uint64
			Period       uint64
		}{
			result.Curb,
			result.FutureTotalCount,
			result.Period,
		})
	}

	httpRenderOutput(w, r, "", nil, &jsonData)
}
