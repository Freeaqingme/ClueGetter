// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package spamassassin

import (
	"fmt"
)

type report struct {
	module *module

	score float64
	facts reportFacts
}

func (report *report) factsAsString() []string {
	out := make([]string, 0)
	for _, fact := range report.facts {
		out = append(out, fact.String())
	}

	return out
}

func (report *report) verdictMessage() string {
	maxScore := 0.0
	msg := "Our system has detected that this message is likely unsolicited mail (SPAM). " +
		"To reduce the amount of spam, this message has been blocked."

	for _, fact := range report.facts {
		value, set := report.module.verdictMsgs[fact.Symbol]
		if !set {
			continue
		}

		if fact.Score > maxScore {
			maxScore = fact.Score
			msg = value
		}
	}

	return msg
}

type reportFacts []reportFact

func (facts reportFacts) Len() int {
	return len(facts)
}

func (facts reportFacts) Less(i, j int) bool {
	return facts[i].Score > facts[j].Score
}

func (facts reportFacts) Swap(i, j int) {
	facts[i], facts[j] = facts[j], facts[i]
}

type reportFact struct {
	Score       float64
	Symbol      string
	Description string
}

func (fact *reportFact) String() string {
	return fmt.Sprintf("%s=%.3f", fact.Symbol, fact.Score)
}
