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
	"cluegetter/core"
	"cluegetter/elasticsearch"
	"math"
	"sort"
	"time"
)

const ModuleName = "reputation"

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
		switch key {
		case "client-address":
		case "sender-sld":
		default:
			m.Log().Fatal("Unknown reputation factor specified: %s", key)
		}
	}

	m.Log().Infof("Registered Reputation Module " + name)

	module := &reputationVolumeModule{
		module: m,

		name: "reputation-volume-" + name,
		conf: conf,
	}
	core.ModuleRegister(module)
	m.modules[name] = module
}

func (m *reputationVolumeModule) Config() *core.ConfigReputationVolume {
	return m.conf
}

func (m *reputationVolumeModule) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	es := (*m.Module("elasticsearch", "reputation")).(*elasticsearch.Module)

	start := time.Now().AddDate(-1, 0, 0) // Now - 1 year
	finder := es.NewFinder()
	finder.SetDateStart(&start)
	finder.SetVerdicts([]int{int(core.Proto_Message_PERMIT)})
	finder.SetLimit(0)

	switch m.Name() {
	case "client-address":
		finder.SetClientAddress(msg.Session().Ip)
	}

	esRes, err := finder.FindWithDateHistogram()
	if err != nil {
		determinants := map[string]interface{}{"error": err.Error()}
		return &core.MessageCheckResult{
			Module:          m.Name(),
			SuggestedAction: core.MessageError,
			Determinants:    determinants,
		}
	}

	series := mapInt64ToSliceInt(esRes.DateHistogram1Yrs)
	total, lastSeries := seriesStats(series)

	isAnomalous := m.isAnomalous(series, m.Config().Minimum_Samples_Yr, m.Config().Volatility, m.Config().No_Stddev)
	determinants := map[string]interface{}{
		"is-anomalous":    isAnomalous,
		"last-30-days":    lastSeries,
		"total-past-year": total,
		"minimum-samples": m.Config().Minimum_Samples_Yr,
		"no-stddev":       m.Config().No_Stddev,
		"volatility":      m.Config().Volatility,
		"penalty":         m.Config().Reject_Score,
		"noop":            m.Config().Noop,
	}

	if m.Config().Noop {
		m.Log().Debugf("Noop: Module %s found message %s to be anomalous: %t", m.Name(), msg.QueueId, isAnomalous)
	} else if isAnomalous {
		m.Log().Debugf("Module %s found message %s to be anomalous: %t", m.Name(), msg.QueueId, isAnomalous)
		return &core.MessageCheckResult{
			Module:          m.Name(),
			SuggestedAction: core.MessageReject,
			Determinants:    determinants,
			Score:           m.Config().Reject_Score,
		}
	}

	return &core.MessageCheckResult{
		Module:          m.Name(),
		SuggestedAction: core.MessagePermit,
		Determinants:    determinants,
	}
}

// Takes past volume of (accepted) mail into account to determine if the
// current volume does not significantly deviate from past patterns.
//
// Exponential Moving Average (EMA)
// EMA <- w * EMA + (1 - w) * x
// w: influence of a new measurement to the EMA
// x: the amount of mail for a particular time slot
//
// Exponential Moving Standard Deviation
// EMS <- sqrt( w * EMS^2 + (1 - w) * (x - EMA)^2 )
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
func (m *module) isAnomalous(in []int, min int, w, n float64) bool {
	ema := 0.0
	ems := 0.0
	total := 0
	for k, x := range in {
		total = total + x
		if k == len(in)-1 {
			// Last item in set should include the message
			// that we're currently checking for
			x = x + 1
		}

		ema = (w * ema) + ((1.0 - w) * float64(x))
		if k == len(in)-1 && total > min && (float64(x)-ema) > (n*ems) {
			return true
		}
		ems = math.Sqrt(w*math.Pow(ems, 2.0) + (1.0-w)*math.Pow(float64(x)-ema, 2))
	}

	return false
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
