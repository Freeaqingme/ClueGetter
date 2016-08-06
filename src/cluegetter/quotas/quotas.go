// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package quotas

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"cluegetter/core"

	"github.com/glenn-brown/golang-pkg-pcre/src/pkg/pcre"
	redis "gopkg.in/redis.v3"
)

const ModuleName = "quotas"

type module struct {
	*core.BaseModule

	getAllQuotasStmt  *sql.Stmt
	getAllRegexesStmt *sql.Stmt

	regexes     []*quotasRegex
	regexesLock *sync.RWMutex
}

func init() {
	core.ModuleRegister(&module{
		BaseModule: core.NewBaseModule(nil),
	})
}

func (m *module) Name() string {
	return ModuleName
}

func (m *module) Enable() bool {
	return m.Config().Quotas.Enabled
}

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

const (
	QUOTA_FACTOR_SENDER           = "sender"
	QUOTA_FACTOR_SENDER_DOMAIN    = "sender_domain"
	QUOTA_FACTOR_RECIPIENT        = "recipient"
	QUOTA_FACTOR_RECIPIENT_DOMAIN = "recipient_domain"
	QUOTA_FACTOR_CLIENT_ADDRESS   = "client_address"
	QUOTA_FACTOR_SASL_USERNAME    = "sasl_username"
)

func (m *module) Init() {
	m.quotasPrepStmt()
	//m.Module("redis", "quotas") // TODO: redis is not amodule, yet

	m.quotasRedisStart()
	m.quotasRegexesStart()
}

func (m *module) HttpHandlers() map[string]core.HttpCallback {
	return map[string]core.HttpCallback{
		"/quotas/sasl_username/": m.quotasSasluserStats,
	}
}

func (m *module) quotasRegexesStart() {
	m.regexesLock = &sync.RWMutex{}

	go func() {
		ticker := time.NewTicker(time.Duration(3) * time.Minute)
		for {
			select {
			case <-ticker.C:
				m.quotasRegexesLoad()
			}
		}
	}()

	go m.quotasRegexesLoad()
}

func (m *module) quotasRegexesLoad() {
	core.CluegetterRecover("quotas.RegexesLoad")

	m.Log().Infof("Importing regexes from RDBMS")
	t0 := time.Now()

	regexes, err := m.getAllRegexesStmt.Query()
	if err != nil {
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
			m.Log().Errorf("Could not compile regex: /%s/. Ignoring. Error: %s", regexStr, err.String())
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

	m.regexesLock.Lock()
	defer m.regexesLock.Unlock()

	m.regexes = regexCollection
	m.Log().Infof("Imported %d regexes in %.2f seconds", i, time.Now().Sub(t0).Seconds())
}

func (m *module) quotasRedisStart() {
	go func() {
		ticker := time.NewTicker(time.Duration(1) * time.Minute)
		for {
			select {
			case <-ticker.C:
				m.quotasRedisUpdateFromRdbms()
			}
		}
	}()

	go m.quotasRedisUpdateFromRdbms()
}

func (m *module) quotasRedisUpdateFromRdbms() {
	core.CluegetterRecover("quotas.RedisUpdateFromRdbms")

	key := fmt.Sprintf("cluegetter-%d-quotas-schedule-quotasRedisUpdateFromRdbms", m.Instance())
	set, err := m.Redis().SetNX(key, m.Hostname(), 5*time.Minute).Result()
	if err != nil {
		m.Log().Errorf("Could not update quotasRedisUpdateFromRdbms schedule: %s", err.Error())
	} else if !set {
		m.Log().Debugf("Update Quotas From Rdbms was run recently. Skipping")
		return
	}

	m.Log().Infof("Importing quotas into Redis")
	t0 := time.Now()

	quotas, err := m.getAllQuotasStmt.Query()
	if err != nil {
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
		key := fmt.Sprintf("{cluegetter-%d-quotas-%s}-definitions", m.Instance(), k)
		m.Redis().Del(key)
		m.Redis().LPush(key, v...)
		m.Redis().Expire(key, time.Duration(24)*time.Hour)
	}

	m.Log().Infof("Imported %d quota tuples into Redis in %.2f seconds", i, time.Now().Sub(t0).Seconds())
}

func (m *module) quotasPrepStmt() {
	stmt, err := m.Rdbms().Prepare(fmt.Sprintf(`
		SELECT q.selector, q.value factorValue, pp.period, pp.curb
			FROM quota q
				LEFT JOIN quota_profile p         ON p.id = q.profile
				LEFT JOIN quota_class c           ON c.id = p.class
				LEFT JOIN quota_profile_period pp ON p.id = pp.profile
			WHERE q.is_regex = 0 AND c.cluegetter_instance = %d
			GROUP BY pp.id, q.id`, m.Instance()))
	if err != nil {
		m.Log().Fatalf("%s", err)
	}
	m.getAllQuotasStmt = stmt

	stmt, err = m.Rdbms().Prepare(fmt.Sprintf(`
		SELECT q.selector, q.value factorValue, pp.period, pp.curb
			FROM quota q
				LEFT JOIN quota_profile p         ON p.id = q.profile
				LEFT JOIN quota_class c           ON c.id = p.class
				LEFT JOIN quota_profile_period pp ON p.id = pp.profile
			WHERE q.is_regex = 1 AND c.cluegetter_instance = %d
			GROUP BY pp.id, q.id`, m.Instance()))
	if err != nil {
		m.Log().Fatalf("%s", err)
	}
	m.getAllRegexesStmt = stmt
}

func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	if !msg.Session().Config().Quotas.Enabled {
		return nil
	}

	c := make(chan *quotasResult)
	var wg sync.WaitGroup

	callbacks := make([]*func(*core.Message, int), 0)
	for selector, selectorValues := range m.quotasGetMsgFactors(msg) {
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
				m.quotasRedisPollQuotasBySelector(c, &lselector, &lselectorValue, m.Instance(), extra_count)
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
			callback := m.quotasRedisAddMsg(result.QuotaKey, i)
			callbacks = append(callbacks, &callback)
		}
	}

	determinants := map[string]interface{}{"quotas": results}

	rejectMsg := ""
	for _, result := range results {
		if result.FutureTotalCount > result.Curb {
			m.Log().Noticef("Quota Exceeding, max of %d messages per %d seconds for %s '%s'",
				result.Curb, result.Period, *result.Selector, *result.FactorValue)
			rejectMsg = fmt.Sprintf("REJECT Policy reject; Exceeding quota, max of %d messages per %d seconds for %s '%s'",
				result.Curb, result.Period, *result.Selector, *result.FactorValue)
		} else {
			m.Log().Infof("Quota Updated, Adding %d message(s) to total of %d (max %d) for last %d seconds for %s '%s'",
				result.ExtraCount, result.FutureTotalCount, result.Curb, result.Period, *result.Selector, *result.FactorValue)
		}
	}

	if rejectMsg != "" {
		return &core.MessageCheckResult{
			Module:          ModuleName,
			SuggestedAction: core.MessageTempFail,
			Message:         rejectMsg,
			Score:           100,
			Determinants:    determinants,
			Callbacks:       callbacks,
		}
	}

	return &core.MessageCheckResult{
		Module:          ModuleName,
		SuggestedAction: core.MessagePermit,
		Message:         "",
		Score:           1,
		Determinants:    determinants,
		Callbacks:       callbacks,
	}
}

func (m *module) quotasRedisPollQuotasBySelector(c chan *quotasResult, selector, selectorValue *string, findInstance uint, extra_count int) {
	key := fmt.Sprintf("{cluegetter-%d-quotas-%s_%s}-definitions", findInstance, *selector, *selectorValue)

	var wg sync.WaitGroup
	i := 0
	for _, quota := range m.Redis().LRange(key, 0, -1).Val() {
		wg.Add(1)
		i++
		go func(quota string) {
			m.quotasRedisPollQuotasBySelectorAndPeriod(c, quota, selector, selectorValue, findInstance, extra_count)
			wg.Done()
		}(quota)
	}

	wg.Wait()

	if i != 0 || findInstance != m.Instance() {
		return
	}

	if !m.quotasRedisInsertRegexesForSelector(selector, selectorValue) {
		return
	}

	for _, quota := range m.Redis().LRange(key, 0, -1).Val() {
		wg.Add(1)
		lquota := quota
		go func() {
			m.quotasRedisPollQuotasBySelectorAndPeriod(c, lquota, selector, selectorValue, m.Instance(), extra_count)
			wg.Done()
		}()
	}

	wg.Wait()
}

func (m *module) quotasRedisInsertRegexesForSelector(selector, selectorValue *string) bool {
	m.regexesLock.RLock()
	regexes := m.regexes
	m.regexesLock.RUnlock()

	inserted := 0
	for _, regex := range regexes {
		if *regex.selector != *selector {
			continue
		}

		regexp := *regex.regex
		if len(regexp.FindIndex([]byte(*selectorValue), 0)) == 0 {
			continue
		}

		key := fmt.Sprintf("{cluegetter-%d-quotas-%s_%s}-definitions", m.Instance(), *selector, *selectorValue)
		m.Redis().LPush(key, fmt.Sprintf("%d_%d", regex.period, regex.curb))
		m.Redis().Expire(key, time.Duration(24)*time.Hour)
		inserted++
	}

	return inserted != 0
}

func (m *module) quotasRedisPollQuotasBySelectorAndPeriod(c chan *quotasResult, quota string,
	selector, selectorValue *string, instance uint, extra_count int) {
	now := time.Now().Unix()
	period, _ := strconv.Atoi(strings.Split(quota, "_")[0])
	curb, _ := strconv.Atoi(strings.Split(quota, "_")[1])
	startPeriod := fmt.Sprintf("%d", now-int64(period))

	quotaKey := fmt.Sprintf("{cluegetter-%d-quotas-%s_%s}-counts-%d", instance, *selector, *selectorValue, period)
	count := m.Redis().ZCount(quotaKey, startPeriod, fmt.Sprintf("%d", now)).Val()
	if (int(count) + extra_count) > curb {
		m.Redis().ZRemRangeByScore(quotaKey, "-inf", startPeriod)
		count = m.Redis().ZCount(quotaKey, startPeriod, fmt.Sprintf("%d", now)).Val()
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

func (m *module) quotasRedisAddMsg(quotasKey *string, count int) func(*core.Message, int) {
	redisKey := quotasKey
	localCount := count

	return func(msg *core.Message, verdict int) {
		if verdict != int(core.Proto_Message_PERMIT) && verdict != int(core.Proto_Message_REJECT) {
			return
		}

		now := time.Now().Unix()
		m.Redis().ZAdd(*redisKey, redis.Z{float64(now), msg.QueueId + "-" + strconv.Itoa(localCount)})
		m.Redis().Expire(*redisKey, 24*time.Hour)
	}
}

func (m *module) quotasGetMsgFactors(msg *core.Message) map[string][]string {
	sess := *msg.Session()
	conf := m.Config().Quotas
	factors := make(map[string][]string)

	if conf.Account_Sender && msg.From.String() != "" {
		factors[QUOTA_FACTOR_SENDER] = []string{msg.From.String()}
	}
	if conf.Account_Sender_Domain {
		factors[QUOTA_FACTOR_SENDER_DOMAIN] = []string{msg.From.Domain()}
	}
	if conf.Account_Recipient {
		rcpts := make([]string, len(msg.Rcpt))
		for k, v := range msg.Rcpt {
			rcpts[k] = v.String()
		}
		factors[QUOTA_FACTOR_RECIPIENT] = rcpts
	}
	if conf.Account_Recipient_Domain {
		rcptDomains := make([]string, len(msg.Rcpt))
		for k, v := range msg.Rcpt {
			rcptDomains[k] = v.Domain()
		}
		factors[QUOTA_FACTOR_RECIPIENT_DOMAIN] = rcptDomains
	}
	if conf.Account_Client_Address {
		factors[QUOTA_FACTOR_CLIENT_ADDRESS] = []string{sess.Ip}
	}
	if conf.Account_Sasl_Username {
		factors[QUOTA_FACTOR_SASL_USERNAME] = []string{sess.SaslUsername}
	}

	return factors
}

func (m *module) quotasSasluserStats(w http.ResponseWriter, r *http.Request) {
	saslUser := r.URL.Path[len("/quotas/sasl_username/"):]
	if len(saslUser) == 0 {
		http.Error(w, "No sasl username provided", http.StatusNotFound)
		return
	}

	if r.FormValue("instance") == "" {
		q := r.URL.Query()
		q.Set("instance", strconv.FormatUint(uint64(m.Instance()), 10))
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
		m.quotasRedisPollQuotasBySelector(results, &selector, &saslUser, uint(instance), 0)
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

	core.HttpRenderOutput(w, r, "", nil, &jsonData)
}
