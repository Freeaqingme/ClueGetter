// ClueGetter - Does things with mail
//
// Copyright 2016 Dolf Schimmel, Freeaqingme.
//
// This Source Code Form is subject to the terms of the two-clause BSD license.
// For its contents, please refer to the LICENSE file.
//
package core

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

var (
	mailQueueDefaultPostsuperExecutable = "/usr/sbin/postsuper"
	mailQueueDefaultPostcatExecutable   = "/usr/sbin/postcat"

	mailQueueNames = []string{"incoming", "active", "deferred", "corrupt", "hold"}
)

func init() {
	deleteQueue := make(chan string)
	enable := func() bool { return Config.MailQueue.Enabled }
	init := func() {
		mailQueueStart(deleteQueue)
	}

	ModuleRegister(&ModuleOld{
		name:   "mailQueue",
		enable: &enable,
		init:   &init,
		rpc: map[string]chan string{
			"mailQueue!delete": deleteQueue,
		},
		httpHandlers: map[string]HttpCallback{
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
		Log.Fatalf("The mailQueue module requires the redis module to be enabled")
	}

	file, err := os.Open(Config.MailQueue.Spool_Dir)
	if err != nil {
		Log.Fatalf("%s could not be opened", Config.MailQueue.Spool_Dir)
	}
	if fi, err := file.Stat(); err != nil || !fi.IsDir() {
		Log.Fatalf("%s could not be opened (or is not a directory)", Config.MailQueue.Spool_Dir)
	}

	for _, queueName := range mailQueueNames {
		go mailQueueUpdate(queueName)
		go func(queueName string) {
			ticker := time.NewTicker(time.Duration(Config.MailQueue.Update_Interval) * time.Second)
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
	if !Config.MailQueue.Enabled {
		return
	}

	args := make([]string, 0, len(queueIds)*2)
	for _, queueId := range queueIds {
		if queueId == "" {
			continue
		}
		if queueId == "ALL" || len(queueId) > 20 {
			Log.Noticef("Received invalid queue id '%s'. Ignoring", queueId)
			continue
		}
		args = append(args, "-d", queueId)
	}

	execPath := Config.MailQueue.PostsuperExecutable
	if execPath == "" {
		execPath = mailQueueDefaultPostsuperExecutable
	}

	var logArgs string
	if len(args) > 90 {
		logArgs = strings.Join(args[:90], " ") + "..."
	} else {
		logArgs = strings.Join(args, " ")
	}
	cmd := exec.Command(execPath, args...)
	cmd.Dir = "/"
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		Log.Errorf("Error ocurred while running postsuper (args: '%s'): %s", logArgs, err)
		return
	}

	// If postsuper deleted 0 items, 0 lines are displayed.
	// Otherwise it's no. of lines + 1.
	linesOut := strings.Split(strings.Trim(out.String(), "\r"), "\n")
	amountDeleted := len(linesOut) - 1
	if amountDeleted < 0 {
		amountDeleted = 0
	}

	Log.Noticef("Deleted queue items. %d requested, %d were present and deleted",
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

	senderLocal, senderDomain := messageParseAddress(options.Sender, false)
	rcptLocal, rcptDomain := messageParseAddress(options.Recipient, false)

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
					Log.Errorf("Could not parse json string: %s", err.Error())
					continue
				}

				if options.Sender == "" && options.Recipient == "" {
					out[queueName] = append(out[queueName], item)
					continue
				}

				itemSenderLocal, itemSenderDomain := messageParseAddress(item.Kv["sender"], false)
				itemRcptLocal, itemRcptDomain := messageParseAddress(item.Kv["recipient"], false)

				if ((senderLocal == "" || strings.EqualFold(itemSenderLocal, senderLocal)) &&
					strings.EqualFold(itemSenderDomain, senderDomain)) || options.Sender == "" {

					if ((rcptLocal == "" || strings.EqualFold(itemRcptLocal, rcptLocal)) &&
						strings.EqualFold(itemRcptDomain, rcptDomain)) || options.Recipient == "" {
						out[queueName] = append(out[queueName], item)
					}
				}
			}
		}
	}
	return out
}

func mailQueueUpdate(queueName string) {
	defer CluegetterRecover("mailQueueUpdate")
	t0 := time.Now()

	wg := &sync.WaitGroup{}
	path := fmt.Sprintf("%s/%s/", Config.MailQueue.Spool_Dir, queueName)
	files := make(chan string, 1024)
	envelopes := make(chan *mailQueueItem, 512)

	wg.Add(1)
	go mailQueueProcessFileList(wg, files, path, envelopes)
	go func() {
		defer CluegetterRecover("mailQueueUpdate")
		count := mailQueueAddToRedis(envelopes, queueName)
		Log.Infof("Imported %d items from the '%s' mailqueue into Redis in %.2f seconds",
			count, queueName, time.Now().Sub(t0).Seconds())
	}()

	pathLen := len(path)
	err := filepath.Walk(path, func(path string, f os.FileInfo, err error) error {
		if f == nil {
			Log.Debugf("File %s has gone. Skipping...", path)
			return nil
		}
		if err != nil {
			Log.Debugf("Got error: %s", err)
			return nil
		}

		if !f.IsDir() {
			files <- path[pathLen:]
		}

		return nil
	})
	close(files)

	wg.Wait()
	close(envelopes)

	if err != nil {
		Log.Errorf("Could not walk %s, got error: %s", path, err.Error())
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
		Log.Errorf("Could not update mailqueue %s, got error updating Redis: %s", queueName, err.Error())
	}

	return count
}

func mailQueueProcessFileList(wg *sync.WaitGroup, files chan string, path string, envelopes chan *mailQueueItem) {
	defer CluegetterRecover("mailQueueProcessFileList")

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
	defer CluegetterRecover("mailQueueProcessFiles")

	execPath := Config.MailQueue.PostcatExecutable
	if execPath == "" {
		execPath = mailQueueDefaultPostcatExecutable
	}

	cmd := exec.Command(execPath, append([]string{"-e"}, filesBatch...)...)
	cmd.Dir = path
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		Log.Errorf("Error ocurred while running postcat: %s. Stderr: %s", err, stderr.String())
		return
	}

	for _, envelope := range strings.Split(out.String(), "*** ENVELOPE RECORDS ")[1:] {
		item, err := mailQueueParseEnvelopeString(envelope)
		if err != nil {
			Log.Errorf("Could not parse envelope string: %s", err.Error())
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
				Log.Noticef("Got named_attribute with no value: %s", kv[0])
				continue
			}
		}

		item.Kv[strings.Trim(kv[0], " ")] = strings.Trim(kv[1], " ")
	}

	// Not all queue items have a log_ident entry denoting the queue id.
	if _, ok := item.Kv["log_ident"]; !ok {
		item.Kv["log_ident"] = queueId
	}

	if _, ok := item.Kv["create_time"]; !ok {
		Log.Noticef("Item %s has no field 'create_time'", queueId)
		return item, nil
	}
	tz, _ := time.Now().Local().Zone()
	parsedTime, err := time.Parse("Mon Jan _2 15:04:05 2006 MST", item.Kv["create_time"]+" "+tz)
	if err != nil {
		Log.Noticef("Could not parse time ('%s') for item %s, error: %s", item.Kv["create_time"], queueId, err)
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
		HttpViewData: HttpGetViewData(),
		Instances:    httpGetInstances(),
	}

	selectedInstances, err := HttpParseFilterInstance(r)
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

	HttpRenderOutput(w, r, "mailQueue.html", &data, &data.QueueItems)
}

func mailQueueHttpDelete(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	for k, v := range r.Form {
		if k != "queueId[]" {
			continue
		}

		err := redisPublish("cluegetter!!mailQueue!delete", []byte(strings.Join(v, " ")))
		if err != nil {
			panic(err)
		}
	}
}
