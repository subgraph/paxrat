// AUthor: David McKinney <mckinney@subgraph>
// Copyright (C) 2014-2015 Subgraph

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"
)

func createTestConfig(path string, contents string) error {
	err := ioutil.WriteFile(path, []byte(contents), 0600)
	if err != nil {
		return err
	}
	return nil
}

func TestRunWatcher1(t *testing.T) {
	dir, err := ioutil.TempDir("", "inotify")
	if err != nil {
		t.Fatalf("TempDir failed: %s\n", err)
	}
	defer os.RemoveAll(dir)
	files := []string{dir + "/test1", dir + "/test2"}
	for _, file := range files {
		_, err = os.OpenFile(file, os.O_WRONLY|os.O_CREATE, 000)
		if err != nil {
			t.Fatalf("creating test file: %s\n", err)
		}
	}
	testJSON := fmt.Sprintf(
		`{"%s/test1": {`+
			`"flags": "mr",`+
			`"nonroot": false},`+
			`"%s/test2": {`+
			`"flags": "E",`+
			`"nonroot": false}}`, dir, dir)
	configPath := dir + "paxrat_conf.json"
	//Conf = new(Config)
	err = createTestConfig(configPath, testJSON)
	if err != nil {
		t.Fatalf("Could not create test config: %s\n", err)
	}
	conf, err := readConfig(configPath)
	if err != nil {
		t.Fatalf("Could not load config: %s\n", err)
	}
	watcher, err := initWatcher(conf)
	if err != nil {
		t.Fatalf("Failed to init watcher: %s\n", err)
	}
	done := make(chan bool)
	go func(done chan bool) {
		runWatcher(watcher, conf)
	}(done)
	err = os.Remove(files[0])
	if err != nil {
		t.Fatalf("Could not remove testFile1: %s\n", err)
	}
	err = os.Rename(files[1], dir+"moved")
	if err != nil {
		t.Fatalf("Could not move/rename TestFile2: %s\n", err)
	}
	_, err = os.OpenFile(files[0], os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		t.Fatalf("creating test file: %s\n", err)
	}
	time.Sleep(1 * time.Second)
}

func TestRunWatcher2(t *testing.T) {
	dir, err := ioutil.TempDir("", "inotify")
	if err != nil {
		t.Fatalf("TempDir failed: %s\n", err)
	}
	defer os.RemoveAll(dir)
	testJSON := fmt.Sprintf(
		`{"%s/1/2/3/4/5/6/7/8/9/10/test1": {`+
			`"flags": "mr",`+
			`"nonroot": false}}`, dir)
	configPath := dir + "paxrat_conf.json"
	err = createTestConfig(configPath, testJSON)
	if err != nil {
		t.Fatalf("Could not create test config: %s\n", err)
	}
	conf, err := readConfig(configPath)
	if err != nil {
		t.Fatalf("Could not load config: %s\n", err)
	}
	watcher, err := initWatcher(conf)
	if err != nil {
		fmt.Println(dir)
		t.Fatalf("Failed to init watcher: %s\n", err)
	}
	done := make(chan bool)
	go func(done chan bool) {
		runWatcher(watcher, conf)
	}(done)
	time.Sleep(1 * time.Second)
	os.MkdirAll(dir+"/1/2/3/4/5/6/7/8/9/10", 0600)
	time.Sleep(1 * time.Second)
	file := dir + "/1/2/3/4/5/6/7/8/9/10/test1"
	fmt.Printf("Creating test file: %s\n", file)
	os.OpenFile(file, os.O_WRONLY|os.O_CREATE, 000)
	if err != nil {
		t.Fatalf("creating test file: %s\n", err)
	}
}
