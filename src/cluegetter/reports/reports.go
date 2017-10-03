// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package reports

import (
	"fmt"
	"io/ioutil"
	"net/mail"
	"strings"
	"time"

	"cluegetter/core"

	"github.com/Freeaqingme/go-pop3"
)

const ModuleName = "reports"

type module struct {
	*core.BaseModule
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
	return m.Config().Reports.Enabled
}

func (m *module) Init() error {
	m.Module("elasticsearch", "reports") // Ensure elasticsearch module is loaded

	for name, config := range m.Config().Reports_Source {
		if config.Type != "pop3" {
			return fmt.Errorf("Only supported values for '%s' type are: 'pop3'. Got: %s", name, config.Type)
		}

		client, err := m.getClient(config)
		if err != nil {
			return fmt.Errorf("Could not connect to pop3 server: %s", err.Error())
		}

		client.Quit()
		client.Close()

		go func(name string) {
			m.fetchReportsFromSource(name)
			ticker := time.NewTicker(time.Duration(1) * time.Minute)
			for {
				select {
				case <-ticker.C:
					m.fetchReportsFromSource(name)
				}
			}
		}(name)

	}

	return nil
}

func (m *module) fetchReportsFromSource(sourceName string) {
	core.CluegetterRecover("reports.fetchReportsFromSource." + sourceName)
	if !m.shouldFetchReports(sourceName) {
		return
	}

	conf := m.Config().Reports_Source[sourceName]
	client, err := m.getClient(conf)
	if err != nil {
		m.Log().Errorf("Could not connect to '%s': %s", sourceName, err.Error())
	}

	defer func() {
		client.Quit()
		client.Close()
	}()

	messageInfo, err := client.UidlAll()
	if err != nil {
		m.Log().Errorf("Could not fetch reports from '%s': %s", sourceName, err.Error())
		return
	}

	m.Log().Info("Found %d reports from '%s'", len(messageInfo), sourceName)

	for _, mi := range messageInfo {
		err := m.fetchAndParseReportFromSource(sourceName, client, mi)
		if err != nil {
			m.Log().Error("Error while fetching from '%s': %s", sourceName, err.Error())
		}
	}
}

func (m *module) fetchAndParseReportFromSource(source string, client *pop3.Client, mi pop3.MessageInfo) error {
	data, err := client.Retr(mi.Number)
	if err != nil {
		return fmt.Errorf("Could not RETR message '%s' from '%s': %s", mi.Uid, err.Error())
	}
	m.dumpReport(source, data, mi.Uid)

	msg, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		return fmt.Errorf("Could not read message '%s': %s", mi.Uid, err.Error())
	}

	res := m.parseAndStoreDmarcMessage(msg)
	if !res {
		return fmt.Errorf("Could not parse '%s' as DMARC", mi.Uid)
	} else {
		m.Log().Infof("Successfully parsed report %s as DMARC", mi.Uid)
	}

	return nil
}

func (m *module) dumpReport(source, data, uid string) {
	dir := m.Config().Reports_Source[source].Dump_Dir
	if dir == "" {
		return
	}

	filename := fmt.Sprintf("cluegetter-dumpReport-%s-%s", source, uid)
	f, err := ioutil.TempFile(dir, filename)
	if err != nil {
		m.Log().Errorf("Could not open file '%s/%s': %s", dir, filename, err.Error())
		return
	}

	defer f.Close()
	count, err := f.WriteString(data)
	if err != nil {
		m.Log().Errorf("Wrote %d bytes to '%s', then got error: %s", count, f.Name(), err.Error())
		return
	}

	m.Log().Debugf("Wrote %d bytes to '%s'", count, f.Name())
}

func (m *module) getClient(config *core.ConfigReportsSource) (*pop3.Client, error) {
	client, err := pop3.Dial(config.Host)
	if err != nil {
		return nil, err
	}

	if err = client.User(config.User); err != nil {
		return nil, err
	}

	if err = client.Pass(config.Password); err != nil {
		return nil, err
	}

	return client, nil
}

func (m *module) shouldFetchReports(name string) bool {
	key := fmt.Sprintf("cluegetter-%d-reports-schedule-fetchReports-%s", m.Instance(), name)
	set, err := m.Redis().SetNX(key, m.Hostname(), 1*time.Minute).Result()
	if err != nil {
		m.Log().Errorf("Could not update fetch reports '%s' schedule: %s", name, err.Error())
		return false
	} else if !set {
		m.Log().Debugf("FetchReports for %s was run recently. Skipping", name)
		return false
	}

	return true
}
