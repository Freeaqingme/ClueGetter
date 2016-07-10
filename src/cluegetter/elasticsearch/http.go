// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package elasticsearch

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"cluegetter/address"
	"cluegetter/core"
)

func (m *module) HttpHandlers() map[string]core.HttpCallback {
	return map[string]core.HttpCallback{
		"/es/message/searchEmail/": func(w http.ResponseWriter, r *http.Request) {
			m.httpHandlerMessageSearchEmail(w, r)
		},
		"/search/": func(w http.ResponseWriter, r *http.Request) {
			m.httpHandlerMessageSearch(w, r)
		},
	}
}

func (m *module) httpHandlerMessageSearch(w http.ResponseWriter, r *http.Request) {
	viewData := struct {
		*core.HttpViewData
		Instances []*core.HttpInstance

		Finder  *Finder
		Results *FinderResponse

		DateHistogram24HrsJsonStr  string
		DateHistogram30DaysJsonStr string
		DateHistogram1YrsJsonStr   string
	}{
		HttpViewData: core.HttpGetViewData(),
		Instances:    core.HttpGetInstances(),

		Finder: m.NewFinder(),
		Results: &FinderResponse{
			Sessions: make([]session, 0),
		},
	}
	viewData.HttpViewData.TplRendersFullBody = true
	instances, err := core.HttpParseFilterInstance(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	core.HttpSetSelectedInstances(viewData.Instances, instances)

	r.ParseForm()
	if r.URL.RawQuery != "" {
		//dateStartStr := r.FormValue("dateStart")
		//dateEndStr:= r.FormValue("dateEnd")
		f := viewData.Finder
		f = f.SetFrom(address.FromAddressOrDomain(r.FormValue("from")))
		f = f.SetTo(address.FromAddressOrDomain(r.FormValue("to")))
		f = f.SetSaslUser(r.FormValue("saslUser"))
		f = f.SetClientAddress(r.FormValue("clientAddress"))
		f = f.SetInstances(instances)

		viewData.Results, err = f.Find()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		jsonBytes, _ := json.Marshal(viewData.Results.DateHistogram24Hrs)
		viewData.DateHistogram24HrsJsonStr = string(jsonBytes)

		jsonBytes, _ = json.Marshal(viewData.Results.DateHistogram30Days)
		viewData.DateHistogram30DaysJsonStr = string(jsonBytes)

		jsonBytes, _ = json.Marshal(viewData.Results.DateHistogram1Yrs)
		viewData.DateHistogram1YrsJsonStr = string(jsonBytes)

		if r.FormValue("json") == "1" {
			// API is not considered stable, but this part even less
			viewData.Results.DateHistogram24Hrs = nil
			viewData.Results.DateHistogram30Days = nil
			viewData.Results.DateHistogram1Yrs = nil
		}

		if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
			core.HttpRenderTemplates(w, r,
				[]string{"elasticsearch/msgResults.html"},
				"elasticsearch/msgResultsWrapper.html",
				viewData,
				viewData.Results,
			)
			return
		}
	}

	core.HttpRenderTemplates(w, r,
		[]string{"elasticsearch/search.html", "elasticsearch/msgResults.html"},
		"skeleton.html",
		viewData,
		viewData.Results,
	)
}

func (m *module) httpHandlerMessageSearchEmail(w http.ResponseWriter, r *http.Request) {
	address := address.FromAddressOrDomain(r.URL.Path[len("/es/message/searchEmail/"):])

	instances, err := core.HttpParseFilterInstance(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sessions, err := m.getSessionsByAddress(instances, address)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	messages := make([]*core.Message, 0)
	for _, sess := range sessions {
		for _, msg := range sess.Messages {
			messages = append(messages, msg)
		}
	}

	// Ideally we'd just pass the messages slice to the view, but to be able
	// to put this live more quickly we'll just use the old (legacy) interface
	// for now. Also keep in mind API BC.
	viewData := struct {
		*core.HttpViewData
		Messages []*core.HttpMessage
	}{
		HttpViewData: core.HttpGetViewData(),
		Messages:     httpHydrateLegacyViewObject(messages),
	}

	core.HttpRenderOutput(w, r, "messageSearchEmail.html", viewData, viewData.Messages)
}

func httpHydrateLegacyViewObject(messages []*core.Message) []*core.HttpMessage {
	out := make([]*core.HttpMessage, 0)

	for _, msg := range messages {
		sess := msg.Session()
		recipients := make([]*core.HttpMessageRecipient, 0)
		for _, rcpt := range msg.Rcpt {
			recipients = append(recipients, &core.HttpMessageRecipient{
				Local:  rcpt.Local(),
				Domain: rcpt.Local(),
				Email:  rcpt.String(),
			})
		}

		headers := make([]*core.HttpMessageHeader, 0)
		for _, hdr := range msg.Headers {
			headers = append(headers, &core.HttpMessageHeader{
				Name: hdr.Key,
				Body: hdr.Value,
			})
		}

		checkResults := make([]*core.HttpMessageCheckResult, 0)
		for _, res := range msg.CheckResults {
			determinantStr, _ := json.Marshal(res.Determinants)
			verdict := strings.ToLower(core.Proto_Message_Verdict_name[int32(res.SuggestedAction)])
			checkResults = append(checkResults, &core.HttpMessageCheckResult{
				Module:        res.Module,
				Verdict:       verdict,
				Score:         res.Score,
				WeightedScore: res.WeightedScore,
				Duration:      res.Duration.Seconds(),
				Determinants:  string(determinantStr),
			})
		}

		verdict := strings.ToLower(core.Proto_Message_Verdict_name[int32(msg.Verdict)])
		out = append(out, &core.HttpMessage{
			Recipients:   recipients,
			Headers:      headers,
			CheckResults: checkResults,

			Ip:            sess.Ip,
			ReverseDns:    sess.ReverseDns,
			Helo:          sess.Helo,
			SaslUsername:  sess.SaslUsername,
			SaslMethod:    sess.SaslMethod,
			CertIssuer:    sess.CertIssuer,
			CertSubject:   sess.CertSubject,
			CipherBits:    strconv.Itoa(int(sess.CipherBits)),
			Cipher:        sess.Cipher,
			TlsVersion:    sess.TlsVersion,
			MtaHostname:   sess.MtaHostName,
			MtaDaemonName: sess.MtaDaemonName,

			Id:         msg.QueueId,
			SessionId:  string(sess.Id()), // TODO Encode?
			Date:       &msg.Date,
			BodySize:   uint32(msg.BodySize),
			Sender:     msg.From.String(),
			RcptCount:  len(msg.Rcpt),
			Verdict:    verdict,
			VerdictMsg: msg.VerdictMsg,

			RejectScore:            msg.RejectScore,
			RejectScoreThreshold:   msg.RejectScoreThreshold,
			TempfailScore:          msg.TempfailScore,
			TempfailScoreThreshold: msg.TempfailScoreThreshold,
			ScoreCombined:          (msg.RejectScore + msg.TempfailScore),
		})
	}

	return out
}
