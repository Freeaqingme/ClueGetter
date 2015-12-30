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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var mailQueueNames = []string{"incoming", "active", "deferred", "corrupt", "hold"}

func init() {
	init := mailQueueStart

	ModuleRegister(&module{
		name: "mailQueue",
		init: &init,
	})
}

type mailQueueItem struct {
	Time time.Time // can be nil
	Kv   map[string]string
}

func mailQueueStart() {
	if Config.MailQueue.Enabled != true {
		Log.Info("Skipping MailQueue module because it was not enabled in the config")
		return
	}

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
		// todo: add recover() - to _all_ goroutines?
		go mailQueueUpdate(queueName)
	}

	Log.Info("MailQueue module started successfully")
}

func mailQueueUpdate(queueName string) {
	t0 := time.Now()

	wg := &sync.WaitGroup{}
	path := fmt.Sprintf("%s/%s/", Config.MailQueue.Spool_Dir, queueName)
	files := make(chan string, 1024)
	envelopes := make(chan *mailQueueItem, 512)

	wg.Add(1)
	go mailQueueProcessFileList(wg, files, path, envelopes)
	go func() {
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

	// begin transaction
	// delete $key
	for envelope := range envelopes {
		count++
		// add items to $key
	}
	// set ttl on $key
	// commit

	return count
}

func mailQueueProcessFileList(wg *sync.WaitGroup, files chan string, path string, envelopes chan *mailQueueItem) {
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
	cmd := exec.Command("postcat", append([]string{"-e"}, filesBatch...)...)
	cmd.Dir = path
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		Log.Error("Error ocurred while running postcat: %s", err)
		// TODO: Now what?
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
