# paxrat

paxrat is a utility to set PaX flags on a set of binaries. 

Subgraph OS uses paxrat to maintain the PaX flags while running in installed and 
live mode. It should also work out of the box on other Debian-based operating
systems. Other Linux variants have not tested but in theory it should also work
provided the paths in the config file are correct (as well as the hard-coded
path to the `paxctl` binary).

## Use cases

paxrat is designed to address a number of use cases currently not supported by
other utilities with a similar purpose.

It supports the following use cases:

1. Running in file-systems that support extended file attributes as well as
those that don't (such as SquashFS in a live disc or docker container)
2. Runnable as a hook to a package manager such as dpkg
3. Runnable in inotify-based watcher mode to set flags when files have changed 
such as during system updates (similar to 
[paxctld](https://grsecurity.net/download.php))
4. Setting flags on a batch of binaries or just one

# Configuration

paxrat configuration is provided via a JSON file that lists each binary, the 
PaX flags, and a `nonroot` setting to specify whether the target binary is 
not root-owned (paxrat will not set PaX flags on non-root owned binaries unless
this is set to `true`). By default paxrat will look for binary divertions using
`dpkg-divert`, this can be disabled by using the `nodivert` setting.

## Configuration example

The following is an example configuration:
```json
{
  "/usr/lib/iceweasel/iceweasel": {                                                     
    "flags": "pm"
  },                                                                            
  "/usr/lib/iceweasel/plugin-container": {                                                                  
    "flags": "m"
  },
  "/home/user/.local/share/torbrowser/tbb/x86_64/tor-browser_en-US/Browser/firefox": {
    "flags": "pm",
    "nonroot": true
  }
}
```

# Usage

## Set flags on a single binary

```sh
$ sudo paxrat -s pm -b /usr/lib/iceweasel/iceweasel 
```

## Set all flags from a config file

```sh
$ sudo paxrat -c paxrat.conf 
```
 
## Test to make sure the provided config file is valid

```sh
$ sudo paxrat -c paxrat.conf -t
```

## Run in watcher mode
```sh
$ sudo paxrat -c paxrat.conf -w
```
