# paxrat

paxrat is a utility to set PaX flags on a set of binaries.

## Use cases

paxrat is designed to address a number of use cases currently not supported by
other utilities with a similar purpose.

It supports the following use cases:
1. Running in file-systems that support extended file attributes as well as
those that don't (such as SquashFS via a live disc or docker container)
2. Runnable as a hook to a package manager such as dpkg
3. Runnable in daemon mode, watching for changed binaries with inotify
4. Setting flags on a batch of binaries or just one



