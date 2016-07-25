// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the Apache License, Version 2.0.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strconv"
	"strings"
	"time"

	"cluegetter/assets"

	proxyproto "github.com/Freeaqingme/go-proxyproto"
)

type HttpCallback func(w http.ResponseWriter, r *http.Request)

func httpStart(done <-chan struct{}) {
	if !Config.Http.Enabled {
		Log.Infof("HTTP module has not been enabled. Skipping...")
		return
	}

	http.HandleFunc("/stats/abusers/", httpAbusersHandler)
	http.HandleFunc("/", httpIndexHandler)

	for _, module := range cg.Modules() {
		for url, callback := range module.HttpHandlers() {
			http.HandleFunc(url, callback)
		}
	}

	for name, httpConfig := range Config.HttpFrontend {
		httpStartFrontend(done, name, httpConfig)
	}

	// Legacy reasons, remove later
	if Config.Http.Listen_Port != "0" {
		httpStartFrontend(done, "LegacyDefaultFrontend", &ConfigHttpFrontend{
			Enabled:     Config.Http.Enabled,
			Listen_Port: Config.Http.Listen_Port,
			Listen_Host: Config.Http.Listen_Host,
		})
	}
}

type HttpViewData struct {
	Config             *config
	Cg                 *Cluegetter
	TplRendersFullBody bool
}

type HttpInstance struct {
	Id          string
	Name        string
	Description string
	Selected    bool
}

type HttpMessage struct {
	Recipients   []*HttpMessageRecipient
	Headers      []*HttpMessageHeader
	CheckResults []*HttpMessageCheckResult

	Ip           string
	ReverseDns   string
	Helo         string
	SaslUsername string
	SaslMethod   string
	CertIssuer   string
	CertSubject  string
	CipherBits   string
	Cipher       string
	TlsVersion   string

	MtaHostname   string
	MtaDaemonName string

	Id                     string
	SessionId              string
	Date                   *time.Time
	BodySize               uint32
	BodySizeStr            string
	Sender                 string
	RcptCount              int
	Verdict                string
	VerdictMsg             string
	RejectScore            float64
	RejectScoreThreshold   float64
	TempfailScore          float64
	TempfailScoreThreshold float64
	ScoreCombined          float64
}

type HttpMessageRecipient struct {
	Id     int
	Local  string
	Domain string
	Email  string
}

type HttpMessageHeader struct {
	Name string
	Body string
}

type HttpMessageCheckResult struct {
	Module        string
	Verdict       string
	Score         float64
	WeightedScore float64
	Duration      float64
	Determinants  string
}

func httpStartFrontend(done <-chan struct{}, name string, httpConfig *ConfigHttpFrontend) {
	if !httpConfig.Enabled {
		Log.Infof("HTTP frontend '%s' has not been enabled. Skipping...", name)
		return
	}

	listener := httpListen(name, httpConfig)
	if httpConfig.Enable_Proxy_Protocol {
		proxyListener := &proxyproto.Listener{listener}
		go http.Serve(proxyListener, httpLogRequest(name, http.DefaultServeMux))
	} else {
		go http.Serve(listener, httpLogRequest(name, http.DefaultServeMux))
	}

	go func() {
		<-done
		listener.Close()
		Log.Infof("HTTP frontend '%s' closed", name)
	}()
}

func httpListen(name string, httpConfig *ConfigHttpFrontend) *net.TCPListener {
	listen_host := httpConfig.Listen_Host
	listen_port := httpConfig.Listen_Port

	laddr, err := net.ResolveTCPAddr("tcp", listen_host+":"+listen_port)
	if nil != err {
		Log.Fatalf(fmt.Sprintf("HTTP Frontend '%s': %s", name, err.Error()))
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if nil != err {
		Log.Fatalf(fmt.Sprintf("HTTP Frontend '%s': %s", name, err.Error()))
	}
	Log.Infof("HTTP frontend '%s' now listening on %s", name, listener.Addr())

	return listener
}

func httpLogRequest(frontend string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Log.Infof("HTTP Request '%s': %s %s %s \"%s\"",
			frontend, r.RemoteAddr, r.Method, r.URL, r.Header.Get("User-Agent"))
		handler.ServeHTTP(w, r)
	})
}

func httpReturnJson(w http.ResponseWriter, obj interface{}) {
	jsonStr, _ := json.Marshal(obj)
	fmt.Fprintf(w, string(jsonStr))
}

func httpIndexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "Page Not Found", http.StatusNotFound)
		return
	}

	http.Redirect(w, r, "/message/search", http.StatusFound)
}

type httpAbuserSelector struct {
	Name     string
	Text     string
	Selected bool
}

type httpAbuserTop struct {
	SenderDomain string
	SaslUsername string
	Count        int
}

func HttpGetViewData() *HttpViewData {
	return &HttpViewData{
		Config: &Config,
		Cg:     cg,
	}
}

func httpAbusersHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	data := struct {
		*HttpViewData
		Instances       []*HttpInstance
		Period          string
		Threshold       string
		SenderDomainTop []*httpAbuserTop
		Selectors       []*httpAbuserSelector
	}{
		HttpGetViewData(),
		HttpGetInstances(),
		"4",
		"5",
		make([]*httpAbuserTop, 0),
		make([]*httpAbuserSelector, 0),
	}

	selectors, err := httpGetSelectors(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	data.Selectors = selectors

	selectedInstances, err := HttpParseFilterInstance(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	HttpSetSelectedInstances(data.Instances, selectedInstances)

	period := r.FormValue("period")
	if _, err := strconv.ParseFloat(period, 64); err != nil || period == "" {
		period = data.Period
	} else {
		data.Period = period
	}

	threshold := r.FormValue("threshold")
	if _, err := strconv.Atoi(threshold); err != nil || threshold == "" {
		threshold = data.Threshold
	} else {
		data.Threshold = threshold
	}

	selector := "m.sender_domain"
	if r.FormValue("selector") == "sasl_username" {
		selector = "s.sasl_username"
	}

	rows, err := Rdbms.Query(`
		SELECT m.sender_domain, s.sasl_username, count(*) amount
			FROM session s JOIN message m ON m.session = s.id
			WHERE m.date > (? - INTERVAL ? HOUR)
				AND s.cluegetter_instance IN(`+strings.Join(selectedInstances, ",")+`)
				AND (verdict = 'tempfail' or verdict = 'reject')
			GROUP BY `+selector+`
			HAVING amount >= ?
			ORDER BY amount DESC
	`, time.Now(), period, threshold)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		result := &httpAbuserTop{}
		rows.Scan(&result.SenderDomain, &result.SaslUsername, &result.Count)
		data.SenderDomainTop = append(data.SenderDomainTop, result)
	}

	HttpRenderOutput(w, r, "abusers.html", data, data.SenderDomainTop)
}

func httpGetSelectors(r *http.Request) (out []*httpAbuserSelector, err error) {
	var selectors []*httpAbuserSelector

	selectors = append(selectors, &httpAbuserSelector{
		Name:     "sasl_username",
		Text:     "Sasl Username",
		Selected: r.FormValue("selector") == "sasl_username",
	})

	selectors = append(selectors, &httpAbuserSelector{
		Name:     "sender_domain",
		Text:     "Sender domain",
		Selected: r.FormValue("selector") == "sender_domain",
	})

	hasSelectedSelector := false
	for _, selector := range selectors {
		if selector.Selected {
			hasSelectedSelector = true
			break
		}
	}

	if !hasSelectedSelector && r.FormValue("selector") != "" {
		return nil, errors.New("Invalid selector specified: " + r.FormValue("selector"))
	}

	return selectors, nil
}

func HttpGetInstances() []*HttpInstance {
	var instances []*HttpInstance
	rows, err := Rdbms.Query(`SELECT id, name, description FROM instance`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		instance := &HttpInstance{}
		rows.Scan(&instance.Id, &instance.Name, &instance.Description)
		instances = append(instances, instance)
	}

	return instances
}

func HttpParseFilterInstance(r *http.Request) (out []string, err error) {
	r.ParseForm()
	instanceIds := r.Form["instance"]

	if len(instanceIds) == 0 {
		for _, instance := range HttpGetInstances() {
			instanceIds = append(instanceIds, instance.Id)
		}
	}

	if strings.Index(instanceIds[0], ",") != -1 {
		instanceIds = strings.Split(instanceIds[0], ",")
	}

	for _, instance := range instanceIds {
		i, err := strconv.ParseInt(instance, 10, 64)
		if err != nil {
			return nil, errors.New("Non-numeric instance identifier found")
		}
		out = append(out, strconv.Itoa(int(i)))
	}
	return
}

func HttpSetSelectedInstances(instances []*HttpInstance, selectedInstances []string) {
	if len(selectedInstances) == 0 {
		for _, instance := range instances {
			instance.Selected = true
		}
	} else {
		for _, selectedInstance := range selectedInstances {
			for _, instance := range instances {
				if instance.Id == selectedInstance {
					instance.Selected = true
				}
			}
		}
	}
}

func HttpRenderOutput(w http.ResponseWriter, r *http.Request, templateFile string, data, jsonData interface{}) {
	HttpRenderTemplates(w, r, []string{templateFile}, "skeleton.html", data, jsonData)
}

func HttpRenderTemplates(w http.ResponseWriter, r *http.Request, templateFiles []string, skeleton string, data, jsonData interface{}) {
	if r.FormValue("json") == "1" {
		if jsonData == nil {
			http.Error(w, "No parameter 'json' supported", http.StatusBadRequest)
			return
		}
		httpReturnJson(w, jsonData)
		return
	} else if len(templateFiles) == 0 || templateFiles[0] == "" {
		http.Error(w, "Parameter 'json' required", http.StatusBadRequest)
		return
	} else if data == nil {
		http.Error(w, "Data was nil for non-json request", http.StatusInternalServerError)
		return
	}

	tplSkeleton, _ := assets.Asset("htmlTemplates/" + skeleton)
	tpl := template.New(skeleton)

	tpl.Funcs(template.FuncMap{
		"jsonEncode": func(input interface{}) string {
			ret, _ := json.Marshal(input)
			return string(ret)
		},
	})

	tpl.Parse(`{{$renderFullBody := false }}`)
	for _, page := range templateFiles {
		tplPage, _ := assets.Asset("htmlTemplates/" + page)
		tpl.Parse(string(tplPage))
	}
	tpl.Parse(string(tplSkeleton))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, skeleton, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
