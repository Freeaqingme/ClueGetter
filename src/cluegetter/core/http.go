// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package core

import (
	"cluegetter/assets"
	proxyproto "github.com/Freeaqingme/go-proxyproto"
	humanize "github.com/dustin/go-humanize"

	"database/sql"
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
)

type HttpCallback func(w http.ResponseWriter, r *http.Request)

func httpStart(done <-chan struct{}) {
	if !Config.Http.Enabled {
		Log.Info("HTTP module has not been enabled. Skipping...")
		return
	}

	http.HandleFunc("/stats/abusers/", httpAbusersHandler)
	http.HandleFunc("/message/", httpHandlerMessage)
	http.HandleFunc("/message/searchEmail/", httpHandlerMessageSearchEmail)
	http.HandleFunc("/message/searchClientAddress/", httpHandlerMessageSearchClientAddress)
	http.HandleFunc("/message/searchSaslUser/", httpHandleMessageSearchSaslUser)
	http.HandleFunc("/", httpIndexHandler)

	for _, module := range modules {
		if !module.Enable() {
			continue
		}

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
	Config *config
}

type httpInstance struct {
	Id          string
	Name        string
	Description string
	Selected    bool
}

type httpMessage struct {
	Recipients   []*httpMessageRecipient
	Headers      []*httpMessageHeader
	CheckResults []*httpMessageCheckResult

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

type httpMessageRecipient struct {
	Id     int
	Local  string
	Domain string
	Email  string
}

type httpMessageHeader struct {
	Name string
	Body string
}

type httpMessageCheckResult struct {
	Module        string
	Verdict       string
	Score         float64
	WeightedScore float64
	Duration      float64
	Determinants  string
}

func httpStartFrontend(done <-chan struct{}, name string, httpConfig *ConfigHttpFrontend) {
	if !httpConfig.Enabled {
		Log.Info("HTTP frontend '%s' has not been enabled. Skipping...", name)
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
		Log.Info("HTTP frontend '%s' closed", name)
	}()
}

func httpListen(name string, httpConfig *ConfigHttpFrontend) *net.TCPListener {
	listen_host := httpConfig.Listen_Host
	listen_port := httpConfig.Listen_Port

	laddr, err := net.ResolveTCPAddr("tcp", listen_host+":"+listen_port)
	if nil != err {
		Log.Fatal(fmt.Sprintf("HTTP Frontend '%s': %s", name, err.Error()))
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if nil != err {
		Log.Fatal(fmt.Sprintf("HTTP Frontend '%s': %s", name, err.Error()))
	}
	Log.Info("HTTP frontend '%s' now listening on %s", name, listener.Addr())

	return listener
}

func httpLogRequest(frontend string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Log.Info("HTTP Request '%s': %s %s %s \"%s\"",
			frontend, r.RemoteAddr, r.Method, r.URL, r.Header.Get("User-Agent"))
		handler.ServeHTTP(w, r)
	})
}

func httpHandlerMessageSearchEmail(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Path[len("/message/searchEmail/"):]
	var local, domain string
	if strings.Index(address, "@") != -1 {
		local = strings.Split(address, "@")[0]
		domain = strings.Split(address, "@")[1]
	} else {
		domain = address
	}

	if local == "" {
		http.Error(w, "Only full addresses supported when searching through RDBMS :(", http.StatusBadRequest)
		return
	}

	instances, err := httpParseFilterInstance(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rows, err := Rdbms.Query(`
		(
			SELECT m.id, m.date, CONCAT(m.sender_local, '@', m.sender_domain) sender, m.rcpt_count, m.verdict,
				GROUP_CONCAT(DISTINCT IF(r.domain = '', r.local, (CONCAT(r.local, '@', r.domain)))) recipients
			FROM message m
				INNER JOIN session s ON s.id = m.session
				INNER JOIN message_recipient mr on mr.message = m.id
				INNER JOIN recipient r ON r.id = mr.recipient
			WHERE (m.sender_domain = ? AND (m.sender_local = ? OR ? = '')) AND
					s.cluegetter_instance IN (`+strings.Join(instances, ",")+`)
				AND m.date >= CURDATE()-INTERVAL 1 WEEK
			GROUP BY m.id
			ORDER BY date DESC
			LIMIT 0,250
		) UNION (
		    SELECT m.id, m.date, CONCAT(m.sender_local, '@', m.sender_domain) sender, m.rcpt_count, m.verdict,
				GROUP_CONCAT(DISTINCT IF(r.domain = '', r.local, (CONCAT(r.local, '@', r.domain)))) recipients
			FROM message m
				INNER JOIN session s ON s.id = m.session
				INNER JOIN message_recipient mr on mr.message = m.id
				INNER JOIN recipient r ON r.id = mr.recipient
			WHERE (r.domain = ? AND (r.local = ? OR ? = ''))
				AND s.cluegetter_instance IN (`+strings.Join(instances, ",")+`)
				AND m.date >= CURDATE()-INTERVAL 1 WEEK
			GROUP BY m.id
			ORDER BY date DESC
			LIMIT 0,250
		)
		ORDER BY date DESC
		LIMIT 0,250
	`, domain, local, local, domain, local, local)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	httpProcessSearchResultRows(w, r, rows)
}

func httpHandlerMessageSearchClientAddress(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Path[len("/message/searchClientAddress/"):]

	instances, err := httpParseFilterInstance(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rows, err := Rdbms.Query(`
		SELECT m.id, m.date, m.sender_local || '@' || m.sender_domain sender, m.rcpt_count, m.verdict,
			GROUP_CONCAT(distinct IF(r.domain = '', r.local, (r.local || '@' || r.domain))) recipients
			FROM message m
				LEFT JOIN message_recipient mr on mr.message = m.id
				LEFT JOIN recipient r ON r.id = mr.recipient
				LEFT JOIN session s ON m.session = s.id
			WHERE s.ip = ? AND s.cluegetter_instance IN (`+strings.Join(instances, ",")+`)
			GROUP BY m.id ORDER BY date DESC LIMIT 0,250
	`, net.ParseIP(address).String())
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	httpProcessSearchResultRows(w, r, rows)
}

func httpHandleMessageSearchSaslUser(w http.ResponseWriter, r *http.Request) {
	saslUser := r.URL.Path[len("/message/searchSaslUser/"):]

	instances, err := httpParseFilterInstance(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rows, err := Rdbms.Query(`
		SELECT m.id, m.date, m.sender_local || '@' || m.sender_domain sender, m.rcpt_count, m.verdict,
			GROUP_CONCAT(distinct IF(r.domain = '', r.local, (r.local || '@' || r.domain))) recipients
			FROM message m
				LEFT JOIN message_recipient mr on mr.message = m.id
				LEFT JOIN recipient r ON r.id = mr.recipient
				LEFT JOIN session s ON m.session = s.id
			WHERE s.sasl_username = ? AND s.cluegetter_instance IN (`+strings.Join(instances, ",")+`)
			GROUP BY m.id ORDER BY date DESC LIMIT 0,250
	`, saslUser)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	httpProcessSearchResultRows(w, r, rows)
}

func httpProcessSearchResultRows(w http.ResponseWriter, r *http.Request, rows *sql.Rows) {
	messages := make([]*httpMessage, 0)
	for rows.Next() {
		message := &httpMessage{Recipients: make([]*httpMessageRecipient, 0)}
		var rcptsStr string
		rows.Scan(&message.Id, &message.Date, &message.Sender, &message.RcptCount,
			&message.Verdict, &rcptsStr)
		for _, rcpt := range strings.Split(rcptsStr, ",") {
			message.Recipients = append(message.Recipients, &httpMessageRecipient{Email: rcpt})
		}
		messages = append(messages, message)
	}

	data := struct {
		*HttpViewData
		Messages []*httpMessage
	}{
		HttpViewData: httpGetViewData(),
		Messages:     messages,
	}

	httpRenderOutput(w, r, "messageSearchEmail.html", data, data.Messages)
}

func httpReturnJson(w http.ResponseWriter, obj interface{}) {
	jsonStr, _ := json.Marshal(obj)
	fmt.Fprintf(w, string(jsonStr))
}

func httpHandlerMessage(w http.ResponseWriter, r *http.Request) {
	queueId := r.URL.Path[len("/message/"):]
	instances, err := httpParseFilterInstance(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	row := Rdbms.QueryRow(`
		SELECT m.session, m.date, COALESCE(m.body_size,0), CONCAT(m.sender_local, '@', m.sender_domain) sender,
				m.rcpt_count, m.verdict, m.verdict_msg,
				COALESCE(m.rejectScore,0), COALESCE(m.rejectScoreThreshold,0), COALESCE(m.tempfailScore,0),
				(COALESCE(m.rejectScore,0) + COALESCE(m.tempfailScore,0)) scoreCombined,
				COALESCE(m.tempfailScoreThreshold,0), s.ip, s.reverse_dns, s.helo, s.sasl_username,
				s.sasl_method, s.cert_issuer, s.cert_subject, s.cipher_bits, s.cipher, s.tls_version,
				cc.hostname mtaHostname, cc.daemon_name mtaDaemonName
			FROM message m
				LEFT JOIN session s ON s.id = m.session
				LEFT JOIN cluegetter_client cc on s.cluegetter_client = cc.id
			WHERE s.cluegetter_instance IN (`+strings.Join(instances, ",")+`)
				AND m.id = ?`, queueId)
	msg := &httpMessage{Recipients: make([]*httpMessageRecipient, 0)}
	err = row.Scan(&msg.SessionId, &msg.Date, &msg.BodySize, &msg.Sender, &msg.RcptCount,
		&msg.Verdict, &msg.VerdictMsg, &msg.RejectScore, &msg.RejectScoreThreshold,
		&msg.TempfailScore, &msg.ScoreCombined, &msg.TempfailScoreThreshold,
		&msg.Ip, &msg.ReverseDns, &msg.Helo, &msg.SaslUsername, &msg.SaslMethod,
		&msg.CertIssuer, &msg.CertSubject, &msg.CipherBits, &msg.Cipher, &msg.TlsVersion,
		&msg.MtaHostname, &msg.MtaDaemonName)
	if err != nil {
		http.Error(w, "Page Not Found: "+err.Error(), http.StatusNotFound)
		return
	}
	msg.BodySizeStr = humanize.Bytes(uint64(msg.BodySize))

	recipientRows, _ := Rdbms.Query(
		"SELECT r.id, r.local, r.domain FROM recipient r "+
			"LEFT JOIN message_recipient mr ON mr.recipient = r.id "+
			"LEFT JOIN message m ON m.id = mr.message WHERE message = ?", queueId)
	defer recipientRows.Close()
	for recipientRows.Next() {
		recipient := &httpMessageRecipient{}
		recipientRows.Scan(&recipient.Id, &recipient.Local, &recipient.Domain)
		if recipient.Domain == "" {
			recipient.Email = recipient.Local
		} else {
			recipient.Email = recipient.Local + "@" + recipient.Domain
		}
		msg.Recipients = append(msg.Recipients, recipient)
	}

	headerRows, _ := Rdbms.Query("SELECT name, body FROM message_header WHERE message = ?", queueId)
	defer headerRows.Close()
	for headerRows.Next() {
		header := &httpMessageHeader{}
		headerRows.Scan(&header.Name, &header.Body)
		msg.Headers = append(msg.Headers, header)
	}

	checkResultRows, err := Rdbms.Query(
		`SELECT module, verdict, score, weighted_score, COALESCE(duration, 0.0),
			determinants FROM message_result WHERE message = ?`, queueId)
	if err != nil {
		panic("Error while retrieving check results in httpHandlerMessage(): " + err.Error())
	}
	defer checkResultRows.Close()
	for checkResultRows.Next() {
		checkResult := &httpMessageCheckResult{}
		checkResultRows.Scan(&checkResult.Module, &checkResult.Verdict, &checkResult.Score,
			&checkResult.WeightedScore, &checkResult.Duration, &checkResult.Determinants)
		msg.CheckResults = append(msg.CheckResults, checkResult)
	}

	data := struct {
		*HttpViewData
		Message *httpMessage
	}{
		HttpViewData: httpGetViewData(),
		Message:      msg,
	}

	httpRenderOutput(w, r, "message.html", data, msg)
}

func httpIndexHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if r.URL.Path != "/" {
		http.Error(w, "Page Not Found", http.StatusNotFound)
		return
	}
	filter := "instance=" + strings.Join(r.Form["instance"], ",")

	if r.FormValue("queueId") != "" {
		http.Redirect(w, r, "/message/"+r.FormValue("queueId")+"?"+filter, http.StatusFound)
		return
	}

	if r.FormValue("mailAddress") != "" {
		http.Redirect(w, r, "/message/searchEmail/"+r.FormValue("mailAddress")+"?"+filter, http.StatusFound)
		return
	}

	if r.FormValue("clientAddress") != "" {
		http.Redirect(w, r, "/message/searchClientAddress/"+r.FormValue("clientAddress")+"?"+filter, http.StatusFound)
		return
	}

	if r.FormValue("saslUser") != "" {
		http.Redirect(w, r, "/message/searchSaslUser/"+r.FormValue("saslUser")+"?"+filter, http.StatusFound)
		return
	}

	data := struct {
		*HttpViewData
		Instances []*httpInstance
	}{
		HttpViewData: httpGetViewData(),
		Instances:    httpGetInstances(),
	}

	httpRenderOutput(w, r, "index.html", data, nil)
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

func httpGetViewData() *HttpViewData {
	return &HttpViewData{
		Config: &Config,
	}
}

func httpAbusersHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	data := struct {
		*HttpViewData
		Instances       []*httpInstance
		Period          string
		Threshold       string
		SenderDomainTop []*httpAbuserTop
		Selectors       []*httpAbuserSelector
	}{
		httpGetViewData(),
		httpGetInstances(),
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

	selectedInstances, err := httpParseFilterInstance(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	httpSetSelectedInstances(data.Instances, selectedInstances)

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

	httpRenderOutput(w, r, "abusers.html", data, data.SenderDomainTop)
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

func httpGetInstances() []*httpInstance {
	var instances []*httpInstance
	rows, err := Rdbms.Query(`SELECT id, name, description FROM instance`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		instance := &httpInstance{}
		rows.Scan(&instance.Id, &instance.Name, &instance.Description)
		instances = append(instances, instance)
	}

	return instances
}

func httpParseFilterInstance(r *http.Request) (out []string, err error) {
	r.ParseForm()
	instanceIds := r.Form["instance"]

	if len(instanceIds) == 0 {
		for _, instance := range httpGetInstances() {
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

func httpSetSelectedInstances(instances []*httpInstance, selectedInstances []string) {
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

func httpRenderOutput(w http.ResponseWriter, r *http.Request, templateFile string, data, jsonData interface{}) {
	if r.FormValue("json") == "1" {
		if jsonData == nil {
			http.Error(w, "No parameter 'json' supported", http.StatusBadRequest)
			return
		}
		httpReturnJson(w, jsonData)
		return
	} else if templateFile == "" {
		http.Error(w, "Parameter 'json' required", http.StatusBadRequest)
		return
	} else if data == nil {
		http.Error(w, "Data was nil for non-json request", http.StatusInternalServerError)
		return
	}

	tplPage, _ := assets.Asset("htmlTemplates/" + templateFile)
	tplSkeleton, _ := assets.Asset("htmlTemplates/skeleton.html")
	tpl := template.New("skeleton.html")
	tpl.Parse(string(tplPage))
	tpl.Parse(string(tplSkeleton))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "skeleton.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
