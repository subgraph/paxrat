# paxrat

paxrat is a utility to set PaX flags on a set of binaries. 

Subgraph OS uses paxrat to maintain the PaX flags while running in installed 
and live mode. It should also work out of the box on other Debian-based 
operating systems. Other Linux variants have not been tested but in theory it 
should also work provided the paths in the config file are correct (as well as 
the hard-coded path to the `paxctl` binary).

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

The default configuration file for paxrat is located in 
`/etc/paxrat/paxrat.conf`. Running paxrat with no configuration file argument 
will automatically use this file to set PaX flags.

paxrat also supports optional configuration files from the 
`/etc/paxrat/conf.d/` directory files. This is for user created configuration. 
paxrat must be run with no `-c` argument to use the files in this directory.

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

## Default mode

When paxrat is run without a configuration file (without `-c`) argument, it will use 
the configuration file found in `/etc/paxrat/paxrat.conf` to set PaX flags. 
It will also scan `/etc/paxrat/conf.d/` for additional configuration files. The 
`/etc/paxrat/conf.d/` directory can be used for user configurations. This is 
the *preferred* mode of operation.


```sh
$ sudo paxrat
```

## Set flags on a single binary

```sh
$ sudo paxrat -s pm -b /usr/lib/iceweasel/iceweasel 
```

## Set all flags from a non-default config file

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
