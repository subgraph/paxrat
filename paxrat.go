// Author: David McKinney <mckinney@subgraph>
// Copyright (C) 2014-2017 Subgraph

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"log/syslog"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/fsnotify/fsnotify"
)

const ioctlReadTermios = 0x5401

const configDirectory = "/etc/paxrat/"

var defaultConfigPath = path.Join(configDirectory, "paxrat.conf")
var optionalConfigDirectory = path.Join(configDirectory, "conf.d/")

var configvar string
var testvar bool
var xattrvar bool
var paxctlvar bool
var watchvar bool
var flagsvar string
var binaryvar string
var nonrootvar bool
var nodivertvar bool
var replacementvar string
var quietvar bool
var verbosevar bool
var configs []Config

type Setting struct {
	Flags    string `json:"flags"`
	Nonroot  bool   `json:"nonroot,omitempty"`
	Nodivert bool   `json:"nodivert,omitempty"`
}

type Config struct {
	Settings map[string]Setting
}

var LogWriter *syslog.Writer
var SyslogError error

var commentRegexp = regexp.MustCompile("^[ \t]*#")
var replacementRegexp = regexp.MustCompile("\\$REPLACEMENT")
var pathlineRegexp = regexp.MustCompile("\\\": {$")

func init() {
	LogWriter, SyslogError = syslog.New(syslog.LOG_INFO, "paxrat")
	if SyslogError != nil {
		log.SetOutput(os.Stdout)
	} else {
		log.SetOutput(io.MultiWriter(os.Stdout, LogWriter))
	}
	flag.StringVar(&configvar, "c", defaultConfigPath,
	"Pax flags configuration file")
	flag.BoolVar(&testvar, "t", false,
	"Test the config file and then exit")
	flag.BoolVar(&xattrvar, "x", false,
	"Force use of xattr to set PaX flags")
	flag.BoolVar(&paxctlvar, "p", false,
	"Force use of paxctl to set PaX flags")
	flag.BoolVar(&watchvar, "w", false,
	"Run paxrat in watch mode")
	flag.StringVar(&flagsvar, "s", "",
	"Set PaX flags for a single binary (must also specify binary)")
	flag.BoolVar(&nonrootvar, "n", false,
	"Set nonroot variable for a single binary (needed to set flags on a non-root owned binary)")
	flag.BoolVar(&nodivertvar, "d", false,
	"Disable checking for dpkg-divert original path (generally this should be enabled)")
	flag.StringVar(&binaryvar, "b", "",
	"Path to a binary for use with set option")
	flag.StringVar(&replacementvar, "r", "",
	"Replacement string to use in binary path JSON (ex: $REPLACEMENT in binary path)")
	flag.BoolVar(&quietvar, "q", false,
	"Turn off all output/logging")
	flag.BoolVar(&verbosevar, "v", false,
	"Verbose output mode")

}

func readConfig(path string) (*Config, error) {
	config := new(Config)
	file, err := os.Open(path)
	if err != nil {
		return config, err
	}
	scanner := bufio.NewScanner(file)
	out := ""
	for scanner.Scan() {
		line := scanner.Text()
		if !commentRegexp.MatchString(line) {
			out += line + "\n"
		}
		if replacementvar != "" && pathlineRegexp.MatchString(line) {
			line = replacementRegexp.ReplaceAllLiteralString(line, replacementvar)
			fmt.Println(line)
		}
	}
	var data = &config.Settings
	err = json.Unmarshal([]byte(out), data)
	if err != nil {
		return config, err
	}
	return config, nil
}

func mergeConfigs() *Config {
	config := new(Config)
	config.Settings = make(map[string]Setting)
	for _, conf := range configs {
		for name, setting := range conf.Settings {
			config.Settings[name] = setting
		}
	}
	return config
}

func pathExists(path string) bool {
	result := false
	if _, err := os.Stat(path); err == nil {
		result = true
	}
	return result
}

func getPathDiverted(path string) (string, error) {
	exists := pathExists("/usr/bin/dpkg-divert")
	if !exists {
		log.Println("Warning: dpkg-divert appears to be unavailable!")
		return path, nil
	}
	outp, err := exec.Command("/usr/bin/dpkg-divert", "--truename", path).Output()
	if err != nil {
		return path, err
	}
	return strings.TrimSpace(string(outp)), nil
}

func validateFlags(flags string) error {
	var err error
	match, _ := regexp.MatchString("(?i)[^pemrxs]", flags)
	if match {
		err = fmt.Errorf("bad characters found in PaX flags: %s",
		flags)
		return err
	}
	return nil
}

func checkEmulTramp(flags string) string {
	if strings.Contains(flags, "E") {
		flags = strings.Replace(flags, "e", "", -1)
	} else {
		if !strings.Contains(flags, "e") {
			flags = flags + "e"
		}
	}
	return flags
}

func setWithXattr(path string, flags string) error {
	flags = checkEmulTramp(flags)
	err := syscall.Setxattr(path, "user.pax.flags", []byte(flags), 0)
	return err
}

func setWithPaxctl(path string, flags string) error {
	exists := pathExists("/sbin/paxctl")
	if !exists {
		log.Printf("/sbin/paxctl does not exist, cannot set '%s' PaX flags on %s.\n", flags, path)
		return nil
	}
	flagsFmt := fmt.Sprintf("-%s", flags)
	args := []string{"-c", flagsFmt, path}
	log.Println(args)
	// TODO: Deal with errors from paxctl
	if err := exec.Command("/sbin/paxctl", args...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}

func setFlags(path string, flags string, nonroot, nodivert bool) error {
	root, err := runningAsRoot()
	if !root {
		log.Fatal("paxrat must be run as root to set PaX flags.")
	}
	err = validateFlags(flags)
	if err != nil {
		err = fmt.Errorf("Could not set PaX flags on %s - %s", path, err)
		return err
	}
	supported, err := isXattrSupported()
	if err != nil {
		return err
	}
	if !nonroot && !nodivert {
		path, err = getPathDiverted(path)
		if err != nil {
			log.Println(err)
			return fmt.Errorf("Unable to get real path for %s\n", path)
		}
	}
	fiPath, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if verbosevar {
				log.Printf("%s does not exist, cannot set flags\n", path)
			}
			err = nil
		}
		return err
	}
	linkUid := fiPath.Sys().(*syscall.Stat_t).Uid
	// Report error if nonroot option is not set but the file is owned by a user other than root
	if !nonroot && linkUid > 0 {
		err = fmt.Errorf("Cannot set PaX flags on %s. Owner of symlink did not match owner of symlink target\n", path)
		return err
	}
	// Resolve the symlink target
	realpath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	fiRPath, err := os.Lstat(realpath)
	if err != nil {
		if os.IsNotExist(err) {
			if verbosevar {
				log.Printf("%s does not exist, cannot set flags\n", path)
			}
			err = nil
		}
		return err
	}
	targetUid := fiRPath.Sys().(*syscall.Stat_t).Uid
	// If nonroot is set then report an error if the owner of the file != owner of the symlink target
	if nonroot && targetUid != linkUid {
		err = fmt.Errorf("Cannot set PaX flags on %s. Owner of symlink did not match owner of symlink target\n", path)
		return err
	}
	if supported {
		log.Printf("Setting '%s' PaX flags via xattr on %s\n", flags, path)
		err = setWithXattr(path, flags)
		if err != nil {
			return err
		}
	} else {
		log.Printf("Setting '%s' PaX flags via paxctl on %s\n", flags, path)
		err = setWithPaxctl(path, flags)
		if err != nil {
			listFlags(path)
			return err
		}
	}
	return err
}

func setFlagsWatchMode(watcher *fsnotify.Watcher, path string, flags string, nonroot, nodivert bool) error {
	watcher.Remove(path)
	err := setFlags(path, flags, nonroot, nodivert)
	if err != nil {
		return err
	}
	addWatchToClosestPath(watcher, path)
	return nil
}

func setFlagsFromConfig(conf *Config) {
	for path, setting := range conf.Settings {
		err := setFlags(path, setting.Flags, setting.Nonroot, setting.Nodivert)
		if err != nil {
			log.Println(err)
		}
	}
}

func listFlags(path string) error {
	exists := pathExists(path)
	if !exists {
		log.Printf("%s does not exist, cannot check PaX flags.\n", path)
		return nil
	}
	supported, err := isXattrSupported()
	if err != nil {
		return err
	}
	if supported {
		var flags []byte
		sz, err := syscall.Getxattr(path, "user.pax.flags", flags)
		if err != nil {
			log.Println(err)
		}
		log.Println(sz)
		log.Println(flags)
	} else {
		args := []string{"-v", path}
		log.Println(args)
		exec.Command("/sbin/paxctl").Run()
		fmt.Fprintln(os.Stdout)
		out, err := exec.Command("/sbin/paxctl", args...).Output()
		if err != nil {
			log.Println(err)
		}
		log.Printf("%s\n", out)
	}
	return err
}

func isXattrSupported() (bool, error) {
	var err error
	if xattrvar {
		return true, err
	}
	if paxctlvar {
		return false, err
	}
	result := true
	setXattrErr := syscall.Setxattr("/proc/self/exe", "user.test xattr", []byte("test xattr data"), 0)
	if setXattrErr != nil {
		errno := setXattrErr.(syscall.Errno)
		// syscall.Setxattr will return 'read-only filesystem' errors on a live-disc in live mode
		if errno == syscall.EOPNOTSUPP || errno == syscall.EROFS {
			result = false
		} else {
			err = setXattrErr
		}
	}
	return result, err
}

func runningAsRoot() (bool, error) {
	current, err := user.Current()
	result := false
	if err != nil {
		log.Println(err)
		return result, err
	}
	if current.Uid == "0" && current.Gid == "0" && current.Username == "root" {
		result = true
	}
	return result, err
}

func addWatchToClosestPath(watcher *fsnotify.Watcher, path string) {
	err := watcher.Add(path)
	for err != nil && err == syscall.ENOENT && path != "/" {
		path = filepath.Dir(path)
		if path != "/" {
			err = watcher.Add(path)
		}
	}
}

func initWatcher(config *Config) (*fsnotify.Watcher, error) {
	log.Println("Initializing paxrat watcher")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return watcher, err
	}
	for path, setting := range config.Settings {
		addWatchToClosestPath(watcher, path)
		err = setFlagsWatchMode(watcher, path, setting.Flags, setting.Nonroot, setting.Nodivert)
		if err != nil {
			log.Printf("setFlags error in initWatcher: %s\n", err)
		}
	}
	return watcher, nil
}

// TODO: Resolve some corner cases like watches not set after create, delete, create, move
func runWatcher(watcher *fsnotify.Watcher, config *Config) {
	log.Println("Starting paxrat watcher")
	for {
		select {
		case ev := <-watcher.Events:
			if ev.Op == fsnotify.Create {
				watcher.Add(ev.Name)
				if verbosevar {
					log.Printf("File created: %s\n", ev.Name)
				}
			} else if ev.Op&fsnotify.Remove == fsnotify.Remove {
				if _, ok := (*config).Settings[ev.Name]; ok {
					if verbosevar {
						log.Printf("File deleted: %s\n", ev.Name)
					}
					parent := filepath.Dir(ev.Name)
					watcher.Add(parent)
					continue
				}
			}
			if settings, ok := (*config).Settings[ev.Name]; ok {
				err := setFlagsWatchMode(watcher, ev.Name, settings.Flags, settings.Nonroot, settings.Nodivert)
				if err != nil {
					log.Printf("watch mode setFlags error: %s\n", err)
				}
			}
		case err := <-watcher.Errors:
			log.Printf("watch mode watcher error: %s\n", err)
		}
	}
}

func main() {
	flag.Parse()
	if quietvar && !verbosevar {
		log.SetOutput(ioutil.Discard)
	}
	if xattrvar && paxctlvar {
		log.Fatal("All set flags mode are forced, only one can be forced (xattr|paxctl)\n")
	}
	if testvar {
		log.Printf("Reading config from: %s\n", configvar)
		_, err := readConfig(configvar)
		if err != nil {
			log.Fatalf("Could not read config: %s - %s\n", configvar, err)
		}
		log.Printf("Configuration is valid\n")
		os.Exit(0)
	} else if binaryvar != "" && flagsvar != "" {
		err := setFlags(binaryvar, flagsvar, nonrootvar, nodivertvar)
		if err != nil {
			log.Println(err)
		}
		os.Exit(0)
	} else {
		var mergedConfig *Config
		if xattrvar {
			log.Println("Running forced xattr mode")
		}
		if paxctlvar {
			log.Println("Running forced paxctl mode")
		}
		log.Printf("Reading config from: %s\n", configvar)
		conf, err := readConfig(configvar)
		if err != nil {
			log.Fatalf("Could not read config: %s, %s\n", configvar, err)
		}
		configs = append(configs, (*conf))
		if configvar == defaultConfigPath &&
		pathExists(optionalConfigDirectory) {
			files, err := ioutil.ReadDir(optionalConfigDirectory)
			if err != nil {
				log.Printf("Could not read optional config directory: %s - %s\n", optionalConfigDirectory, err)
			}
			for _, confFile := range files {
				if !confFile.IsDir() {
					confFilePath := path.Join(optionalConfigDirectory,
					confFile.Name())
					log.Printf("Reading config from: %s", confFilePath)
					if pathExists(confFilePath) {
						conf, err := readConfig(confFilePath)
						if err != nil {
							// Do not die fatally on bad optional config
							log.Printf("Could not read config: %s - %s\n",
							confFile.Name(), err)
						}
						configs = append(configs, (*conf))
					}
				}
			}
		}
		mergedConfig = mergeConfigs()
		if watchvar {
			watcher, err := initWatcher(mergedConfig)
			if err != nil {
				log.Fatalf("Could not initialize watcher: %s\n", err)
			}
			runWatcher(watcher, mergedConfig)
		} else {
			setFlagsFromConfig(mergedConfig)
			os.Exit(0)
		}
	}
}
