// ClueGetter - Does things with mail
//
// Copyright 2015 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var mailQueueNames = []string{"incoming", "active", "deferred", "corrupt", "hold"}

func init() {
	deleteQueue := make(chan string)
	enable := func() bool { return Config.MailQueue.Enabled }
	init := func() {
		mailQueueStart(deleteQueue)
	}

	ModuleRegister(&module{
		name:   "mailQueue",
		enable: &enable,
		init:   &init,
		rpc: map[string]chan string{
			"mailQueue!delete": deleteQueue,
		},
		httpHandlers: map[string]httpCallback{
			"/mailqueue":        mailQueueHttp,
			"/mailqueue/delete": mailQueueHttpDelete,
		},
	})
}

type mailQueueItem struct {
	Time time.Time // can be nil
	Kv   map[string]string
}

type mailQueueGetOptions struct {
	Sender    string
	Recipient string
	Instances []string
}

func mailQueueStart(deleteQueue chan string) {
	if !Config.Redis.Enabled {
		Log.Fatal("The mailQueue module requires the redis module to be enabled")
	}

	file, err := os.Open(Config.MailQueue.Spool_Dir)
	if err != nil {
		Log.Fatal("%s could not be opened", Config.MailQueue.Spool_Dir)
	}
	if fi, err := file.Stat(); err != nil || !fi.IsDir() {
		Log.Fatal("%s could not be opened (or is not a directory)", Config.MailQueue.Spool_Dir)
	}

	for _, queueName := range mailQueueNames {
		go mailQueueUpdate(queueName)
		go func(queueName string) {
			ticker := time.NewTicker(time.Duration(30) * time.Second)
			for {
				select {
				case <-ticker.C:
					mailQueueUpdate(queueName)
				}
			}
		}(queueName)
	}

	go mailQueueHandleDeleteChannel(deleteQueue)
}

func mailQueueHandleDeleteChannel(deleteQueue chan string) {
	for queueString := range deleteQueue {
		queueIds := strings.Split(queueString, " ")
		go mailQueueDeleteItems(queueIds)
	}
}

func mailQueueDeleteItems(queueIds []string) {
	args := make([]string, 0, len(queueIds)*2)
	for _, queueId := range queueIds {
		if queueId == "" {
			continue
		}
		if queueId == "ALL" || len(queueId) > 20 {
			Log.Notice("Received invalid queue id '%s'. Ignoring", queueId)
			continue
		}
		args = append(args, "-d", queueId)
	}

	cmd := exec.Command("postsuper", args...)
	cmd.Dir = "/"
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		Log.Error("Error ocurred while running postsuper: %s", err)
		return
	}

	// If postsuper deleted 0 items, 0 lines are displayed.
	// Otherwise it's no. of lines + 1.
	linesOut := strings.Split(strings.Trim(out.String(), "\r"), "\n")
	amountDeleted := len(linesOut) - 1
	if amountDeleted < 0 {
		amountDeleted = 0
	}

	Log.Notice("Deleted queue items. %d requested, %d were present and deleted",
		len(queueIds), amountDeleted)
}

func mailQueueGetFromDataStore(options *mailQueueGetOptions) map[string][]*mailQueueItem {
	serviceInOneOfInstances := func(service *service, instances []string) bool {
		for _, instance := range instances {
			if strconv.Itoa(int(service.Instance)) == instance {
				return true
			}
		}

		return false
	}

	senderLocal, senderDomain := messageParseAddress(options.Sender)
	rcptLocal, rcptDomain := messageParseAddress(options.Recipient)

	out := make(map[string][]*mailQueueItem, 0)
	for _, queueName := range mailQueueNames {
		out[queueName] = make([]*mailQueueItem, 0)

		for _, service := range redisGetServices() {
			if !serviceInOneOfInstances(service, options.Instances) {
				continue
			}

			key := fmt.Sprintf("cluegetter-%d-mailqueue-%s-%s",
				service.Instance, service.Hostname, queueName)

			for _, jsonStr := range redisClient.LRange(key, 0, -1).Val() {
				item := &mailQueueItem{}
				err := json.Unmarshal([]byte(jsonStr), &item)
				if err != nil {
					Log.Error("Could not parse json string: %s", err.Error())
					continue
				}

				if options.Sender == "" && options.Recipient == "" {
					out[queueName] = append(out[queueName], item)
					continue
				}

				itemSenderLocal, itemSenderDomain := messageParseAddress(item.Kv["sender"])
				itemRcptLocal, itemRcptDomain := messageParseAddress(item.Kv["recipient"])

				if ((senderLocal == "" || itemSenderLocal == senderLocal) &&
					itemSenderDomain == senderDomain) || options.Sender == "" {
					if ((rcptLocal == "" || itemRcptLocal == rcptLocal) &&
						itemRcptDomain == rcptDomain) || options.Recipient == "" {
						out[queueName] = append(out[queueName], item)
					}
				}
			}
		}
	}
	return out
}

func mailQueueUpdate(queueName string) {
	defer cluegetterRecover("mailQueueUpdate")
	t0 := time.Now()

	wg := &sync.WaitGroup{}
	path := fmt.Sprintf("%s/%s/", Config.MailQueue.Spool_Dir, queueName)
	files := make(chan string, 1024)
	envelopes := make(chan *mailQueueItem, 512)

	wg.Add(1)
	go mailQueueProcessFileList(wg, files, path, envelopes)
	go func() {
		defer cluegetterRecover("mailQueueUpdate")
		count := mailQueueAddToRedis(envelopes, queueName)
		Log.Info("Imported %d items from the '%s' mailqueue into Redis in %.2f seconds",
			count, queueName, time.Now().Sub(t0).Seconds())
	}()

	pathLen := len(path)
	err := filepath.Walk(path, func(path string, f os.FileInfo, err error) error {
		if !f.IsDir() {
			files <- path[pathLen:]
		}

		return nil
	})
	close(files)

	wg.Wait()
	close(envelopes)

	if err != nil {
		Log.Error("Could not walk %s, got error: %s", path, err.Error())
	}
}

func mailQueueAddToRedis(envelopes chan *mailQueueItem, queueName string) int {
	count := 0
	key := fmt.Sprintf("cluegetter-%d-mailqueue-%s-%s", instance, hostname, queueName)

	tx, _ := redisNewTransaction(key)
	_, err := tx.Exec(func() error {
		tx.Del(key)

		for envelope := range envelopes {
			count++
			envelopeStr, _ := json.Marshal(envelope)
			tx.LPush(key, string(envelopeStr))
		}
		tx.Expire(key, time.Duration(5)*time.Minute)
		return nil
	})

	if err != nil {
		Log.Error("Could not update mailqueue %s, got error updating Redis: %s", queueName, err.Error())
	}

	return count
}

func mailQueueProcessFileList(wg *sync.WaitGroup, files chan string, path string, envelopes chan *mailQueueItem) {
	defer cluegetterRecover("mailQueueProcessFileList")

	filesBatch := make([]string, 0, 256)
	for file := range files {
		filesBatch = append(filesBatch, file)
		if len(filesBatch) >= 256 {
			wg.Add(1)
			go func(filesBatch []string) {
				mailQueueProcessFiles(filesBatch, path, envelopes)
				wg.Done()
			}(filesBatch)
			filesBatch = make([]string, 0, 256)
		}
	}
	go func(filesBatch []string) {
		mailQueueProcessFiles(filesBatch, path, envelopes)
		wg.Done()
	}(filesBatch)
}

func mailQueueProcessFiles(filesBatch []string, path string, envelopes chan *mailQueueItem) {
	defer cluegetterRecover("mailQueueProcessFileList")

	cmd := exec.Command("postcat", append([]string{"-e"}, filesBatch...)...)
	cmd.Dir = path
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		Log.Error("Error ocurred while running postcat: %s", err)
		return
	}

	for _, envelope := range strings.Split(out.String(), "*** ENVELOPE RECORDS ")[1:] {
		item, err := mailQueueParseEnvelopeString(envelope)
		if err != nil {
			Log.Error("Could not parse envelope string: %s", err.Error())
			continue
		}
		envelopes <- item
	}
}

func mailQueueParseEnvelopeString(envelopeStr string) (*mailQueueItem, error) {
	lines := strings.Split(envelopeStr, "\n")
	queueId := strings.SplitN(lines[0], " ", 2)[0]
	if len(queueId) < 6 {
		return nil, fmt.Errorf("Could not determine queue id")
	}

	item := &mailQueueItem{
		Kv: make(map[string]string),
	}
	for _, line := range lines {
		kv := strings.SplitN(line, ": ", 2)
		if len(kv) != 2 {
			continue
		}

		if kv[0] == "named_attribute" {
			kv = strings.SplitN(kv[1], "=", 2)
			if len(kv) != 2 {
				Log.Notice("Got named_attribute with no value: %s", kv[0])
				continue
			}
		}

		item.Kv[strings.Trim(kv[0], " ")] = strings.Trim(kv[1], " ")
	}

	if _, ok := item.Kv["create_time"]; !ok {
		Log.Notice("Item %s has no field 'create_time'", queueId)
		return item, nil
	}
	tz, _ := time.Now().Local().Zone()
	parsedTime, err := time.Parse("Mon Jan _2 15:04:05 2006 MST", item.Kv["create_time"]+" "+tz)
	if err != nil {
		Log.Notice("Could not parse time ('%s') for item %s, error: %s", item.Kv["create_time"], queueId, err)
		return item, nil
	}
	item.Time = parsedTime

	return item, nil
}

func mailQueueHttp(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	data := struct {
		*HttpViewData
		Instances  []*httpInstance
		QueueItems map[string][]*mailQueueItem
		Sender     string
		Recipient  string
	}{
		HttpViewData: httpGetViewData(),
		Instances:    httpGetInstances(),
	}

	selectedInstances, err := httpParseFilterInstance(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	httpSetSelectedInstances(data.Instances, selectedInstances)

	data.Sender = r.FormValue("sender")
	data.Recipient = r.FormValue("recipient")

	data.QueueItems = mailQueueGetFromDataStore(&mailQueueGetOptions{
		Sender:    data.Sender,
		Recipient: data.Recipient,
		Instances: selectedInstances,
	})

	httpRenderOutput(w, r, "mailQueue.html", &data, &data.QueueItems)
}

func mailQueueHttpDelete(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	for k, v := range r.Form {
		if k != "queueId[]" {
			continue
		}

		err := redisClient.Publish("cluegetter!!mailQueue!delete", strings.Join(v, " ")).Err()
		if err != nil {
			panic(err)
		}
	}
}
