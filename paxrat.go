// Author: David McKinney <mckinney@subgraph>
// Copyright (C) 2014-2015 Subgraph

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/syslog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/exp/inotify"
)

var configvar string
var testvar bool
var xattrvar bool
var watchvar bool
var flagsvar string
var binaryvar string
var nonrootvar bool
var nodivertvar bool

type Setting struct {
	Flags    string `json:"flags"`
	Nonroot  bool   `json:"nonroot,omitempty"`
	Nodivert bool   `json:"nodivert,omitempty"`
}
type Config struct {
	Settings map[string]Setting
}

var InotifyFlags uint32
var InotifyDirFlags uint32
var Conf *Config
var LogWriter *syslog.Writer
var SyslogError error

var commentRegexp = regexp.MustCompile("^[ \t]*#")

func init() {
	LogWriter, SyslogError = syslog.New(syslog.LOG_INFO, "paxrat")
	if SyslogError != nil {
		// TODO: We should continue running and log to STDOUT instead using same LogWriter interface
		fmt.Println(SyslogError)
		os.Exit(0) // Exit cleanly for dpkg
	}
	InotifyFlags = (inotify.IN_DONT_FOLLOW | inotify.IN_ATTRIB |
		inotify.IN_CREATE | inotify.IN_DELETE_SELF | inotify.IN_MOVE_SELF |
		inotify.IN_MOVED_TO)
	InotifyDirFlags = (inotify.IN_DONT_FOLLOW | inotify.IN_CREATE |
		inotify.IN_DELETE_SELF | inotify.IN_MOVE_SELF | inotify.IN_MOVED_TO)
	Conf = new(Config)
	flag.StringVar(&configvar, "c", "/etc/paxrat/paxrat.conf",
		"Pax flags configuration file")
	flag.BoolVar(&testvar, "t", false,
		"Test the config file and then exit")
	flag.BoolVar(&xattrvar, "x", false,
	        "Force use of xattr to set PaX flags")
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
}

func (conf *Config) readConfig(path string) (error) {
	file, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	scanner := bufio.NewScanner(file)
	out := ""
	for scanner.Scan() {
		line := scanner.Text()
		if !commentRegexp.MatchString(line) {
			out += line + "\n"
		}

	}
	var data = &conf.Settings
	err = json.Unmarshal([]byte(out), data)
	if err != nil {
		log.Fatal(err)
	}
	return err
}

func pathExists(path string) (bool) {
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

func validateFlags(flags string) (error) {
	var err error
	match, _ := regexp.MatchString("(?i)[^pemrxs]", flags)
	if match {
		err = fmt.Errorf("Bad characters found in PaX flags: %s",
			flags)
	}
	return err
}

func setWithXattr(path string, flags string) (error) {
	err := syscall.Setxattr(path, "user.pax.flags", []byte(flags), 0)
	return err
}

func setWithPaxctl(path string, flags string) (error) {
	exists := pathExists("/sbin/paxctl")
	if !exists {
		msg := fmt.Sprintf(
			"/sbin/paxctl does not exist, cannot set '%s' PaX flags on %s.\n",
			flags, path)
		LogWriter.Debug(msg)
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

func setFlags(path string, flags string, nonroot, nodivert bool) (error) {
	root, err := runningAsRoot()
	if !root {
		log.Fatal("paxrat must be run as root to set PaX flags.")
	}
	err = validateFlags(flags)
	if err != nil {
		return err
	}
	supported, err := isXattrSupported()
	if err != nil {
		return err
	}
	if !nonroot && !nodivert {
		path, err = getPathDiverted(path)
		if err != nil {
			return fmt.Errorf("Unable to get real path for %s", path)
		}
	}
	fiPath, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			msg := fmt.Sprintf("%s does not exist, cannot set flags", path)
			LogWriter.Debug(msg)
			err = nil
		}
		return err
	}
	linkUid := fiPath.Sys().(*syscall.Stat_t).Uid
	// Throw error if nonroot option is not set but the file is owned by a user other than root
	if !nonroot && linkUid > 0 {
		err = fmt.Errorf(
			"Cannot set PaX flags on %s. Owner of symlink did not match owner of symlink target\n",
			path)
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
			msg := fmt.Sprintf("%s does not exist, cannot set flags", path)
			LogWriter.Info(msg)
			err = nil
		}
		return err
	}
	targetUid := fiRPath.Sys().(*syscall.Stat_t).Uid
	// If nonroot is set then throw an error if the owner of the file is different than the owner of the symlink target
	if nonroot && targetUid != linkUid {
		err = fmt.Errorf(
			"Cannot set PaX flags on %s. Owner of symlink did not match owner of symlink target\n",
			path)
		return err
	}
	if supported {
		msg := fmt.Sprintf("Setting '%s' PaX flags via xattr on %s\n", flags, path)
		LogWriter.Info(msg)
		err = setWithXattr(path, flags)
		if err != nil {
			return err
		}
	} else {
		msg := fmt.Sprintf("Setting '%s' PaX flags via paxctl on %s\n", flags, path)
		LogWriter.Info(msg)
		err = setWithPaxctl(path, flags)
		if err != nil {
			listFlags(path)
			return err
		}
	}
	return err
}

func setFlagsWatchMode(watcher *inotify.Watcher, path string, flags string, nonroot, nodivert bool) (error) {
	watcher.RemoveWatch(path)
	err := setFlags(path, flags, nonroot, nodivert)
	if err != nil {
		return err
	}
	addWatchToClosestPath(watcher, path)
	return nil
}

func setFlagsFromConfig() {
	for path, setting := range (*Conf).Settings {
		err := setFlags(path, setting.Flags, setting.Nonroot, setting.Nodivert)
		if err != nil {
			log.Println(err)
		}
	}
}

func listFlags(path string) (error) {
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
		LogWriter.Debug("Running forced xattr mode")
		return true, err
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

func addWatchToClosestPath(watcher *inotify.Watcher, path string) {
	err := watcher.AddWatch(path, InotifyFlags)
	for err != nil && err.(*os.PathError).Err == syscall.ENOENT && path != "/" {
		path = filepath.Dir(path)
		if path != "/" {
			err = watcher.AddWatch(path, InotifyDirFlags)
		}
	}

}

func initWatcher() (*inotify.Watcher, error) {
	LogWriter.Info("Initializing paxrat watcher")
	watcher, err := inotify.NewWatcher()
	if err != nil {
		return watcher, err
	}
	for path, setting := range (*Conf).Settings {
		addWatchToClosestPath(watcher, path)
		err = setFlagsWatchMode(watcher, path, setting.Flags, setting.Nonroot, setting.Nodivert)
		if err != nil {
			msg := fmt.Sprintf("setFlags error in initWatcher: %s", err)
			LogWriter.Err(msg)
		}
	}
	return watcher, nil
}

// TODO: Resolve some corner cases like watches not set after create, delete, create, move
func runWatcher(watcher *inotify.Watcher) {
	LogWriter.Info("Starting paxrat watcher")
	for {
		select {
		case ev := <-watcher.Event:
			if ev.Mask == inotify.IN_CREATE {
				if _, ok := (*Conf).Settings[ev.Name]; ok {
					watcher.AddWatch(ev.Name, InotifyFlags)
					msg := fmt.Sprintf("File created: %s\n", ev.Name)
					LogWriter.Info(msg)
				}
				// Catch directory creation events for non-existent directories in executable path
			} else if ev.Mask == (inotify.IN_CREATE | inotify.IN_ISDIR) {
				for path, _ := range (*Conf).Settings {
					if strings.HasPrefix(path, ev.Name) {
						addWatchToClosestPath(watcher, path)
					}
				}
			} else if ev.Mask == inotify.IN_DELETE_SELF || ev.Mask == inotify.IN_MOVE_SELF {
				if _, ok := (*Conf).Settings[ev.Name]; ok {
					msg := fmt.Sprintf("File deleted: %s\n", ev.Name)
					LogWriter.Info(msg)
					parent := filepath.Dir(ev.Name)
					watcher.AddWatch(parent, InotifyDirFlags)
					continue
				}
			} else if ev.Mask == inotify.IN_ATTRIB {
				if _, ok := (*Conf).Settings[ev.Name]; ok {
					exists := pathExists(ev.Name)
					if !exists {
						msg := fmt.Sprintf("File deleted: %s\n", ev.Name)
						LogWriter.Info(msg)
						parent := filepath.Dir(ev.Name)
						watcher.AddWatch(parent, InotifyDirFlags)
						continue
					} else {
						msg := fmt.Sprintf("File attributes changed: %s", ev.Name)
						LogWriter.Info(msg)
					}
				}
			}
			if settings, ok := (*Conf).Settings[ev.Name]; ok {
				if ev.Mask != inotify.IN_IGNORED {
					err := setFlagsWatchMode(watcher, ev.Name, settings.Flags, settings.Nonroot, settings.Nodivert)
					if err != nil {
						msg := fmt.Sprintf("watch mode setFlags error: %s", err)
						LogWriter.Err(msg)
					}
				}
			}
		case err := <-watcher.Error:
			msg := fmt.Sprintf("watch mode watcher error: %s", err)
			LogWriter.Err(msg)
		}
	}
	return
}

func main() {
	flag.Parse()
	if testvar {
		log.Printf("Reading config from: %s\n", configvar)
		err := Conf.readConfig(configvar)
		if err != nil {
			log.Fatal(err)
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
		log.Printf("Reading config from: %s\n", configvar)
		err := Conf.readConfig(configvar)
		if err != nil {
			log.Fatal(err)
		}
		if watchvar {
			watcher, err := initWatcher()
			if err != nil {
				log.Fatalf("Could not initialize watcher: %s", err)
			}
			runWatcher(watcher)
		} else {
			setFlagsFromConfig()
			os.Exit(0)
		}
	}
}
