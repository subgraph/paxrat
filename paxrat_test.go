package main

import (
	"io/ioutil"
	"os"
	"testing"
	"fmt"
	"time"
)

func createTestConfig(path string, contents string) (err error) {
	err = ioutil.WriteFile(path, []byte(contents), 0600)
	if err != nil {
		return
	}
	return
}

func TestRunWatcher1(t *testing.T) {
	dir, err := ioutil.TempDir("", "inotify")
	if err != nil {
		t.Fatalf("TempDir failed: %s", err)
	}
	defer os.RemoveAll(dir)
	files := []string{dir + "/test1", dir + "/test2"}
	for _, file := range files {
		_, err = os.OpenFile(file, os.O_WRONLY|os.O_CREATE, 000)
		if err != nil {
			t.Fatalf("creating test file: %s", err)
		}
	}
	testJson := fmt.Sprintf(
	"{\"%s/test1\": {" +
	"\"flags\": \"mr\"," +
	"\"nonroot\": false}," +
	"\"%s/test2\": {" +
	"\"flags\": \"E\"," +
	"\"nonroot\": false}}", dir, dir)
	configPath := dir + "paxrat_conf.json"
	Conf = new(Config)
	err = createTestConfig(configPath, testJson)
	if err != nil {
		t.Fatalf("Could not create test config: %s", err)
	}
	err = Conf.readConfig(configPath)
	if err != nil {
		t.Fatalf("Could not load config: %s", err)
	}
	watcher, err := initWatcher()
	if err != nil {
		t.Fatalf("Failed to init watcher: %s", err)
	}
	done := make(chan bool)
	go func(done chan bool) {
		runWatcher(watcher)
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

func TestRunWatcher2(t *testing.T) {
	dir, err := ioutil.TempDir("", "inotify")
	if err != nil {
		t.Fatalf("TempDir failed: %s", err)
	}
	defer os.RemoveAll(dir)
	testJson := fmt.Sprintf(
		"{\"%s/1/2/3/4/5/6/7/8/9/10/test1\": {" +
		"\"flags\": \"mr\"," +
		"\"nonroot\": false}}", dir)
	configPath := dir + "paxrat_conf.json"
	Conf = new(Config)
	err = createTestConfig(configPath, testJson)
	if err != nil {
		t.Fatalf("Could not create test config: %s", err)
	}
	err = Conf.readConfig(configPath)
	if err != nil {
		t.Fatalf("Could not load config: %s", err)
	}
	watcher, err := initWatcher()
	if err != nil {
		fmt.Println(dir)
		t.Fatalf("Failed to init watcher: %s", err)
	}
	done := make(chan bool)
	go func(done chan bool) {
		runWatcher(watcher)
	}(done)
	time.Sleep(1 * time.Second)
	os.MkdirAll(dir + "/1/2/3/4/5/6/7/8/9/10", 0600 )
	time.Sleep(1 * time.Second)
	file := dir + "/1/2/3/4/5/6/7/8/9/10/test1"
	fmt.Printf("Creating test file: %s", file)
	os.OpenFile(file, os.O_WRONLY|os.O_CREATE, 000)
	if err != nil {
		t.Fatalf("creating test file: %s", err)
	}
}

