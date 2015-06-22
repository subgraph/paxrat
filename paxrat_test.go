package main

import (
	"io/ioutil"
	"os"
	"testing"
	"fmt"
	"encoding/json"
	"time"
)


func TestRunWatcher1(t *testing.T) {
	dir, err := ioutil.TempDir("", "inotify")
	if err != nil {
		t.Fatalf("TempDir failed: %s", err)
	}
	defer os.RemoveAll(dir)
	files := []string{dir + "/test1", dir + "/test2"}
	for _, file := range files {
		_, err = os.OpenFile(file, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			t.Fatalf("creating test file: %s", err)
		}
	}
	var data config
	err = json.Unmarshal(
		[]byte(fmt.Sprintf("{ \"%s/test1\":\"E\",\n \"%s/test2\":\"E\" }", dir, dir)), &data)
	if err != nil {
		t.Fatalf("Could not load config: %s", err)
	}
	watcher, err := initWatcher(data)
	if err != nil {
		t.Fatalf("Failed to init watcher: %s", err)
	}
	done := make(chan bool)
	go func(done chan bool) {
		runWatcher(watcher, data)
	}(done)
	err = os.Remove(files[0])
	if err != nil {
		t.Fatalf("Could not remove testFile1: %s", err)
	}
	err = os.Rename(files[1], dir + "moved")
	if err != nil {
		t.Fatalf("Could not move/rename TestFile2: %s", err)
	}
	_, err = os.OpenFile(files[0], os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		t.Fatalf("creating test file: %s", err)
	}
	time.Sleep(1 * time.Second)
}

