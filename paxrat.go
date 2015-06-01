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
)

var configvar string
var daemonvar bool
var flagsvar string
var binaryvar string
type config map[string]string

func init() {
	flag.StringVar(&configvar, "c", "/etc/paxrat/paxrat.conf", 
		"Pax flags configuration file")
	flag.BoolVar(&daemonvar, "d", false, 
		"Run paxrat as a daemon")
	flag.StringVar(&flagsvar, "s", "", 
		"Set PaX flags for a single binary (must also specify binary)")
	flag.StringVar(&binaryvar, "b", "",
		"Path to a binary for use with set option")
}

func ReadConfig(path string) (data config, err error) {
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

func FileExists(path string) (result bool, err error) {
	if _, err := os.Stat(path); err == nil {
		result = true
	}
	return
}

func ValidateFlags(flags string) (err error) {
	match, _ := regexp.MatchString("(?i)[^pemrxs]", flags)
    	if match {
		err = fmt.Errorf("Bad characters found in PaX flags: %s", 
			flags)
	}
	return
}

func SetWithXattr(path string, flags string) (err error) {
	err = syscall.Setxattr(path, "user.pax.flags", []byte(flags), 0)
	return
}

func SetWithPaxctl(path string, flags string) (err error) {
	exists, err := FileExists("/sbin/paxctl")
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

func SetFlags(path string, flags string) (err error) {
	root, err := RunningAsRoot()
	if !root {
		log.Fatal("paxrat must be run as root to set PaX flags.")
	}
	exists, err := FileExists(path)
	if !exists {
		fmt.Printf("%s does not exist, cannot set PaX flags: %s\n",
			path, flags)
		return
	}
	err = ValidateFlags(flags)
	if err != nil {
		return
	}
	supported, err := IsXattrSupported("/bin/ls")
	if err != nil {
		return
	}
	fmt.Printf("Setting '%s' PaX flags on %s\n", flags, path)
	if supported {
		err = SetWithXattr(path, flags)
		if err != nil {
			return
		}
	} else {
		err = SetWithPaxctl(path, flags)
		if err != nil {
			ListFlags(path)
			return
		}
	}
	return
}

func SetFlagsFromConfig(data config) {
	for path, flags := range data {
		err := SetFlags(path, flags)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func ListFlags(path string) (err error) {
	exists, err := FileExists(path)
	if !exists {
		fmt.Printf("%s does not exist, cannot check PaX flags.\n",
			path)
		return
	}
	supported, err := IsXattrSupported("/bin/ls")
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

func IsXattrSupported(path string) (result bool, err error) {
	result = true
	setXattrErr := syscall.Setxattr(path, "user.test xattr", []byte("test xattr data"), 0)
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

func RunningAsRoot() (result bool, err error) {
	current, err := user.Current()
	if err != nil {
		fmt.Println(err)
	}
	if current.Uid == "0" && current.Gid == "0" && current.Username == "root" {
		result = true
	}
	return
}

func main() {
	flag.Parse()
	if binaryvar != "" && flagsvar != "" {
		SetFlags(binaryvar, flagsvar)
	} else {
		fmt.Printf("Reading config from: %s\n", configvar)
		data, err := ReadConfig(configvar)
		if err != nil {
			log.Fatal(err)
		}
		SetFlagsFromConfig(data)
	}
}
