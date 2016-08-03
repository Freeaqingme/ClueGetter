// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package ipinfo

import (
	"fmt"
	"net"
	"time"

	"cluegetter/core"

	"github.com/ammario/ipisp"
	"github.com/oschwald/geoip2-golang"
	"sync"
)

const ModuleName = "ipinfo"

type module struct {
	*core.BaseModule

	ipispClient *ipisp.Client

	geoliteDb *geoip2.Reader
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
	return m.Config().Ipinfo.Enabled
}

func (m *module) Init() {
	var err error
	m.ipispClient, err = ipisp.NewClient()
	if err != nil {
		m.Log().Fatal("Could not initiate ipisp client: " + err.Error())
	}

	m.geoliteDb, err = geoip2.Open(m.Config().Ipinfo.Geolite_Db)
	if err != nil {
		m.Log().Fatal(err.Error())
	}
}

func (m *module) Stop() {
	m.ipispClient.Close()
	m.geoliteDb.Close()

}

// TODO: We don't need to do this in the MessageCheck step, we could also do it earlier
// as some connect hook. If only we had one.
func (m *module) MessageCheck(msg *core.Message, done chan bool) *core.MessageCheckResult {
	ip := net.ParseIP("149.210.181.20") ///////////////// TODO
	info:= &core.IpInfo{}

	wg := &sync.WaitGroup{}
	wg.Add(2)
	go func() {
		info.ASN, info.ISP, info.IpRange, info.AllocationDate= m.ipisp(ip)
		wg.Done()
	}()

	go func() {
		info.Country, info.Continent, info.Lat, info.Long = m.geoip(ip)
		wg.Done()
	}()

	wg.Wait()

	msg.Session().IpInfo = info

	determinants := map[string]interface{}{
		"info": info,
	}

	return &core.MessageCheckResult{
		Module:          ModuleName,
		SuggestedAction: core.MessagePermit,
		Message:         "",
		Score:           0,
		Determinants:    determinants,
	}
}

func (m *module) geoip(ip net.IP) (country, continent string, lat, long float64) {
	r, err := m.geoliteDb.City(ip)
	if err != nil {
		m.Log().Fatal(err.Error())
	}

	return r.Country.IsoCode, r.Continent.Code, r.Location.Latitude, r.Location.Longitude
}

func (m *module) ipisp(ip net.IP) (asn, isp, ipRange string, allocationDate *time.Time) {
	resp, err := m.ipispClient.LookupIP(ip)
	if err != nil {
		fmt.Println(err.Error())
	}

	return resp.ASN.String(), resp.Name.Raw, resp.Range.String(), &resp.Allocated
}
