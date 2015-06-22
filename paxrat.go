package main

import (
	"flag"
	"io/ioutil"
	"encoding/json"
	"regexp"
	"fmt"
	"log"
	"syscall"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"golang.org/x/exp/inotify"
)

var configvar string
var watchvar bool
var flagsvar string
var binaryvar string
type config map[string]string
var InotifyFlags uint32
var InotifyDirFlags uint32

func init() {
	InotifyFlags = (inotify.IN_DONT_FOLLOW | inotify.IN_ATTRIB |
		inotify.IN_CREATE | inotify.IN_DELETE_SELF | inotify.IN_MOVE_SELF |
		inotify.IN_MOVED_TO)
	InotifyDirFlags = (inotify.IN_DONT_FOLLOW | inotify.IN_CREATE |
		inotify.IN_DELETE_SELF | inotify.IN_MOVE_SELF | inotify.IN_MOVED_TO)

	flag.StringVar(&configvar, "c", "/etc/paxrat/paxrat.conf",
		"Pax flags configuration file")
	flag.BoolVar(&watchvar, "w", false,
		"Run paxrat in watch mode")
	flag.StringVar(&flagsvar, "s", "", 
		"Set PaX flags for a single binary (must also specify binary)")
	flag.StringVar(&binaryvar, "b", "",
		"Path to a binary for use with set option")
}

func readConfig(path string) (data config, err error) {
	file, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	err = json.Unmarshal(file, &data)
	if err != nil {
		log.Fatal(err)
	}
	return
}

func pathExists(path string) (result bool) {
	if _, err := os.Stat(path); err == nil {
		result = true
	}
	return
}

func validateFlags(flags string) (err error) {
	match, _ := regexp.MatchString("(?i)[^pemrxs]", flags)
    	if match {
		err = fmt.Errorf("Bad characters found in PaX flags: %s", 
			flags)
	}
	return
}

func setWithXattr(path string, flags string) (err error) {
	err = syscall.Setxattr(path, "user.pax.flags", []byte(flags), 0)
	return
}

func setWithPaxctl(path string, flags string) (err error) {
	exists := pathExists("/sbin/paxctl")
	if !exists {
		fmt.Printf(
			"/sbin/paxctl does not exist, cannot set '%s' PaX flags on %s.\n",
			flags, path)
		return
	}
	flagsFmt := fmt.Sprintf("-%s", flags)
	args := []string{"-c", flagsFmt, path}
	fmt.Println(args)
	// TODO: Deal with errors from paxctl
	if err = exec.Command("/sbin/paxctl", args...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	return
}

func setFlags(path string, flags string) (err error) {
	root, err := runningAsRoot()
	if !root {
		log.Fatal("paxrat must be run as root to set PaX flags.")
	}
	exists := pathExists(path)
	if !exists {
		fmt.Printf("%s does not exist, cannot set PaX flags: %s\n",
			path, flags)
		return
	}
	err = validateFlags(flags)
	if err != nil {
		return
	}
	supported, err := isXattrSupported()
	if err != nil {
		return
	}
	fmt.Printf("Setting '%s' PaX flags on %s\n", flags, path)
	if supported {
		err = setWithXattr(path, flags)
		if err != nil {
			return
		}
	} else {
		err = setWithPaxctl(path, flags)
		if err != nil {
			listFlags(path)
			return
		}
	}
	return
}

func setFlagsFromConfig(data config) {
	for path, flags := range data {
		err := setFlags(path, flags)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func listFlags(path string) (err error) {
	exists := pathExists(path)
	if !exists {
		fmt.Printf("%s does not exist, cannot check PaX flags.\n",
			path)
		return
	}
	supported, err := isXattrSupported()
	if err != nil {
		return
	}
	if supported {
		var flags []byte
		sz, err := syscall.Getxattr(path, "user.pax.flags", flags)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println(sz)
		fmt.Println(flags)
	} else {
		args := []string{"-v", path}
		fmt.Println(args)
		exec.Command("/sbin/paxctl").Run()
		fmt.Fprintln(os.Stdout)
		out, err := exec.Command("/sbin/paxctl", args...).Output()
		if err != nil {
			fmt.Println(err)
		}
		fmt.Printf("%s\n", out)
	}
	return
}

func isXattrSupported() (result bool, err error) {
	result = true
	setXattrErr := syscall.Setxattr("/proc/self/exe", "user.test xattr", []byte("test xattr data"), 0)
	if setXattrErr != nil {
		errno := setXattrErr.(syscall.Errno)
		if errno == syscall.EOPNOTSUPP {
			fmt.Println("xattr not supported in filesystem.")
			result = false
		} else {
			err = setXattrErr
		}
	} else {
		fmt.Println("xattr is supported in filesystem.")
	}
	return
}

func runningAsRoot() (result bool, err error) {
	current, err := user.Current()
	if err != nil {
		fmt.Println(err)
	}
	if current.Uid == "0" && current.Gid == "0" && current.Username == "root" {
		result = true
	}
	return
}

func addWatchToClosestPath(watcher *inotify.Watcher, path string) {
	parent := filepath.Dir(path)
	err := watcher.AddWatch(path, InotifyFlags)
	for !pathExists(parent) && parent != "" {
		path := strings.Split(parent, "/")
		parent = strings.Join(path[:len(path)-1], "/")
	}
	if parent != "" {
		err = watcher.AddWatch(parent, InotifyDirFlags)
		if err != nil {
			fmt.Println(err)
		}
	}

}

func initWatcher(data config) (watcher *inotify.Watcher, err error) {
	watcher, err = inotify.NewWatcher()
	if err != nil {
		return
	}
	for path, flags := range data {
		addWatchToClosestPath(watcher, path)
		watcher.RemoveWatch(path)
		err := setFlags(path, flags)
		if err != nil {
			if err.(syscall.Errno) == syscall.ENOENT {
				fmt.Println("Not found")
			}
			fmt.Println(err)
		}
		addWatchToClosestPath(watcher, path)
	}
	return
}

// TODO: Resolve some corner cases like watches not set after create, delete, create, move
func runWatcher(watcher *inotify.Watcher, data config) {
	log.Printf("Starting paxrat watcher")
	for {
		select {
		case ev := <-watcher.Event:
			if ev.Mask == inotify.IN_CREATE {
					if _, ok := data[ev.Name]; ok {
					watcher.AddWatch(ev.Name, InotifyFlags)
					log.Printf("File created: %s\n", ev.Name)
				}
			} else if ev.Mask == inotify.IN_DELETE_SELF || ev.Mask == inotify.IN_MOVE_SELF {
				if _, ok := data[ev.Name]; ok {
					log.Printf("File deleted: %s\n", ev.Name)
					parent := filepath.Dir(ev.Name)
					watcher.AddWatch(parent, InotifyDirFlags)
					continue
				}
			} else if ev.Mask == inotify.IN_ATTRIB {
				if _, ok := data[ev.Name]; ok {
					exists := pathExists(ev.Name)
					if !exists {
						log.Printf("File deleted: %s\n", ev.Name)
						parent := filepath.Dir(ev.Name)
						watcher.AddWatch(parent, InotifyDirFlags)
						continue
					} else {
						log.Printf("File attributes changed: %s", ev.Name)
					}
				}
			}
			if _, ok := data[ev.Name]; ok {
				if ev.Mask != inotify.IN_IGNORED {
					watcher.RemoveWatch(ev.Name)
					setFlags(ev.Name, data[ev.Name])
					addWatchToClosestPath(watcher, ev.Name)
				}
			}
		case err := <-watcher.Error:
			log.Println("error:", err)
		}
	}
	return
}

func main() {
	flag.Parse()
	if binaryvar != "" && flagsvar != "" {
		setFlags(binaryvar, flagsvar)
	} else {
		fmt.Printf("Reading config from: %s\n", configvar)
		data, err := readConfig(configvar)
		if err != nil {
			log.Fatal(err)
		}
		if watchvar {
			watcher, err := initWatcher(data)
			if err != nil {
				log.Fatalf("Could not initialize watcher: %s", err)
			}
			runWatcher(watcher, data)
		} else {
			setFlagsFromConfig(data)
		}
	}
}
