// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
// https://medium.com/@iliasfl/data-science-tricks-simple-anomaly-detection-for-metrics-with-a-weekly-pattern-2e236970d77#.utd26zuuk
//
package reputation

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"cluegetter/core"
	"cluegetter/elasticsearch"

	"cluegetter/address"
	"gopkg.in/redis.v3"
)

const ModuleName = "reputation"

const (
	FACTOR_SENDER         = "sender"
	FACTOR_SENDER_DOMAIN  = "sender_domain"
	FACTOR_SENDER_SLD     = "sender_sld"
	FACTOR_CLIENT_ADDRESS = "client_address"
	FACTOR_SASL_USERNAME  = "sasl_username"
	FACTOR_AS             = "as"
	FACTOR_IP_RANGE       = "ip_range"
	FACTOR_COUNTRY        = "country"
)

type module struct {
	*core.BaseModule

	modules map[string]*reputationVolumeModule
}

type reputationVolumeModule struct {
	*module

	name string
	conf *core.ConfigReputationVolume
}

func init() {
	core.ModuleRegister(&module{
		BaseModule: core.NewBaseModule(nil),
	})
}

func (m *module) Name() string {
	return ModuleName
}

func (m *reputationVolumeModule) Name() string {
	return m.name
}

func (m *module) Enable() bool {
	return true
}

func (m *module) Init() {
	m.modules = make(map[string]*reputationVolumeModule, 0)

	for name, conf := range m.Config().Reputation_Volume {
		if !conf.Enabled {
			continue
		}
		m.startModule(name, conf)
	}
}

func (m *module) startModule(name string, conf *core.ConfigReputationVolume) {
	m.Module("elasticsearch", "reputation") // Mark dependency, fail if missing

	for key := range m.Config().Reputation_Volume {
		switch strings.Replace(key, "-", "_", -1) {
		case FACTOR_SENDER:
		case FACTOR_SENDER_DOMAIN:
		case FACTOR_SENDER_SLD:
		case FACTOR_CLIENT_ADDRESS:
		case FACTOR_SASL_USERNAME:
		case FACTOR_AS:
		case FACTOR_IP_RANGE:
		case FACTOR_COUNTRY:
		default:
			m.Log().Fatalf("Unknown reputation factor specified: %s", key)
		}
	}

	m.Log().Infof("Registered Reputation Module " + name)

	module := &reputationVolumeModule{
		module: m,

		name: "reputation-volume-" + strings.Replace(name, "-", "_", -1),
		conf: conf,
	}
	core.ModuleRegister(module)
	m.modules[name] = module
}

func (m *reputationVolumeModule) Config() *core.ConfigReputationVolume {
	return m.conf
}

func (m *reputationVolumeModule) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	series, err := m.getSeries(msg)
	if err != nil {
		determinants := map[string]interface{}{"error": err.Error()}
		return &core.MessageCheckResult{
			Module:          m.Name(),
			SuggestedAction: core.MessageError,
			Determinants:    determinants,
		}
	}

	total, lastSeries := seriesStats(series)
	determinants := map[string]interface{}{
		"last-30-days":    lastSeries,
		"total-past-year": total,
		"minimum-samples": m.Config().Minimum_Samples_Yr,
		"no-stddev":       m.Config().No_Stddev,
		"volatility":      m.Config().Volatility,
		"penalty":         m.Config().Reject_Score,
		"noop":            m.Config().Noop,
	}

	factorValue := m.getFactorValue(msg)
	if factorValue == "" {
		return nil
	}

	callback := func(msg *core.Message, verdict int) {
		if verdict != core.MessagePermit {
			return
		}

		key := fmt.Sprintf("cluegetter-%d-reputation-volume-%s-%s-today",
			m.Instance(),
			m.Name(),
			factorValue,
		)

		m.Redis().Incr(key)
	}

	if total < m.Config().Minimum_Samples_Yr {
		return &core.MessageCheckResult{
			Module:          m.Name(),
			SuggestedAction: core.MessagePermit,
			Determinants:    determinants,
			Score:           m.Config().Reject_Score,
			Callbacks:       []*func(*core.Message, int){&callback},
		}
	}

	isAnomalous := isAnomalous(series, m.Config().Volatility, m.Config().No_Stddev)
	determinants["is-anomalous"] = isAnomalous

	if m.Config().Noop {
		m.Log().Debugf("Noop: Module %s found message %s to be anomalous: %t", m.Name(), msg.QueueId, isAnomalous)
	} else if isAnomalous {
		m.Log().Debugf("Module %s found message %s to be anomalous: %t", m.Name(), msg.QueueId, isAnomalous)
		return &core.MessageCheckResult{
			Module:          m.Name(),
			SuggestedAction: core.MessageReject,
			Determinants:    determinants,
			Score:           m.Config().Reject_Score,
			Callbacks:       []*func(*core.Message, int){&callback},
		}
	}

	return &core.MessageCheckResult{
		Module:          m.Name(),
		SuggestedAction: core.MessagePermit,
		Determinants:    determinants,
		Callbacks:       []*func(*core.Message, int){&callback},
	}
}

func (m *reputationVolumeModule) getSeries(msg *core.Message) ([]int, error) {
	series, redisKey, err := m.getSeriesRedis(msg)
	if err != nil {
		m.Log().Error(err.Error())
	}
	if len(series) == 0 {
		series, err = m.getSeriesES(msg)
		if err != nil {
			return series, err
		}

		m.seriesPersistInRedis(redisKey, series)
	}

	return series, nil
}

func (m *reputationVolumeModule) getSeriesRedis(msg *core.Message) ([]int, string, error) {
	key := fmt.Sprintf("cluegetter-%d-reputation-volume-%s-%s-",
		m.Instance(),
		m.Name(),
		m.getFactorValue(msg),
	)

	res, err := m.Redis().Get(key + "series").Result()
	if err != nil {
		if err == redis.Nil {
			return []int{}, key, nil
		}
		return []int{}, key, fmt.Errorf("Could not get -series from Redis: %s", err.Error())
	}

	out := make([]int, 0)
	parts := strings.Split(res, ",")
	for _, part := range parts {
		val, _ := strconv.Atoi(part)
		out = append(out, val)
	}

	today := 0
	res, err = m.Redis().Get(key + "today").Result()
	if err != nil {
		if err == redis.Nil {
			return []int{}, key, nil
		}
		return []int{}, key, fmt.Errorf("Could not get -today from Redis: %s", err.Error())
	} else {
		today, _ = strconv.Atoi(res)
	}

	return append(out, today), key, nil
}

func (m *reputationVolumeModule) getSeriesES(msg *core.Message) ([]int, error) {
	es := (*m.Module("elasticsearch", "reputation")).(*elasticsearch.Module)

	start := time.Now().AddDate(-1, 0, 0) // Now - 1 year
	finder := es.NewFinder()
	finder.SetInstances([]string{strconv.Itoa(int(m.Instance()))})
	finder.SetDateStart(&start)
	finder.SetVerdicts([]int{int(core.Proto_Message_PERMIT)})
	finder.SetLimit(0)

	factorValue := m.getFactorValue(msg)
	switch m.Name()[len("reputation-volume-"):] {
	case FACTOR_SENDER:
		finder.SetFrom(address.FromString(factorValue))
	case FACTOR_SENDER_DOMAIN:
		finder.SetFrom(address.FromAddressOrDomain(factorValue))
	case FACTOR_SENDER_SLD:
		finder.SetFromSld(factorValue)
	case FACTOR_CLIENT_ADDRESS:
		finder.SetClientAddress(factorValue)
	case FACTOR_SASL_USERNAME:
		finder.SetSaslUser(factorValue)
	case FACTOR_AS:
		finder.SetAS(factorValue)
	case FACTOR_IP_RANGE:
		finder.SetIpRange(factorValue)
	case FACTOR_COUNTRY:
		finder.SetCountry(factorValue)
	}

	esRes, err := finder.FindWithDateHistogram()
	if err != nil {
		return nil, err
	}

	return mapInt64ToSliceInt(esRes.DateHistogram1Yrs), nil
}

func (m *reputationVolumeModule) getFactorValue(msg *core.Message) string {
	switch m.Name()[len("reputation-volume-"):] {
	case FACTOR_SENDER:
		return msg.From.String()
	case FACTOR_SENDER_DOMAIN:
		return msg.From.Domain()
	case FACTOR_SENDER_SLD:
		return msg.From.Sld()
	case FACTOR_CLIENT_ADDRESS:
		return msg.Session().Ip
	case FACTOR_SASL_USERNAME:
		return msg.Session().SaslUsername
	case FACTOR_AS:
		return msg.Session().IpInfo.ASN
	case FACTOR_IP_RANGE:
		return msg.Session().IpInfo.IpRange
	case FACTOR_COUNTRY:
		return msg.Session().IpInfo.Country
	}

	panic("Unknown module name: " + m.Name())
}

// Takes past volume of (accepted) mail into account to determine if the
// current volume does not significantly deviate from past patterns.
//
// Threshold is exceeded when: (x - EMA) > (n * EMS)
// n: Number of standard deviations compared to
//    the difference between EMA and x.
//
// Some examples use ABS(x - EMA) instead, but we don't
// care about decrease in values, only in increases.
//
// We could in theory also show what the maximum would be.
// Probably something along the lines of (thank wolframalpha):
// (x-(w * EMA + (1 - w) * x)) = (n*sqrt( w * EMS^2 + (1 - w) * (x - EMA)^2 ))
// n sqrt((1-w) (EMA^2+x^2)+w (2 EMA x+EMS^2)-2 EMA x) = w (x-EMA)
//
// w = (n^2 (-(EMA^2-2 EMA x+x^2-EMS^2))-sqrt(n^4 (EMA^2-2 EMA x+x^2-EMS^2)^2+4 n^2 (EMA^2-2 EMA x+x^2)^2))
// 					/ (2 (EMA^2-2 EMA x+x^2))
//
func isAnomalous(in []int, w, n float64) bool {
	ema := 0.0
	ems := 0.0
	var k, x int

	for k, x = range in {
		if k == len(in)-1 {
			break
		}

		ema = calcEma(x, ema, w)
		ems = calcEms(x, ema, ems, w)
	}

	ema = calcEma(x, ema, w)
	if (float64(x) - ema) > (n * ems) {
		return true
	}

	return false
}

// Exponential Moving Average (EMA)
// EMA <- w * EMA + (1 - w) * x
// w: influence of a new measurement to the EMA
// x: the amount of mail for a particular time slot
func calcEma(x int, ema, w float64) float64 {
	return (w * ema) + ((1.0 - w) * float64(x))
}

// Exponential Moving Standard Deviation
// EMS <- sqrt( w * EMS^2 + (1 - w) * (x - EMA)^2 )
//
func calcEms(x int, ema, ems, w float64) float64 {
	return math.Sqrt(w*math.Pow(ems, 2.0) + (1.0-w)*math.Pow(float64(x)-ema, 2))
}

func (m *reputationVolumeModule) seriesPersistInRedis(redisKey string, series []int) {
	seriesString := make([]string, len(series))
	for k, v := range series {
		seriesString[k] = strconv.Itoa(v)
	}

	if len(series) == 0 {
		m.Redis().Set(redisKey+"series", "0", 4*time.Hour)
		m.Redis().Set(redisKey+"today", "0", 4*time.Hour)
		return
	}

	seriesContents := strings.Join(seriesString[0:len(series)-1], ",")
	m.Redis().Set(redisKey+"series", seriesContents, 4*time.Hour)
	m.Redis().Set(redisKey+"today", series[len(series)-1], 4*time.Hour)
}

func mapInt64ToSliceInt(in map[int64]int64) []int {
	out := make([]int, len(in))
	keys := make([]int, len(in))
	i := 0
	for k := range in {
		keys[i] = int(k)
		i++
	}

	sort.Ints(keys)

	i = 0
	for _, k := range keys {
		out[i] = int(in[int64(k)])
		i++
	}

	return out
}

func seriesStats(series []int) (int, []int) {
	total := 0
	for _, v := range series {
		total = total + v
	}

	var lastSeries []int
	if len(series) < 30 {
		lastSeries = series
	} else {
		lastSeries = series[len(series)-30:]
	}

	return total, lastSeries
}
