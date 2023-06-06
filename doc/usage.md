# Usage

## Command line

### Conventions and autonomy

#### Flags

Command line flags use the [Go standard
convention](https://pkg.go.dev/flag#hdr-Command_line_flag_syntax) and `fs` provides a `-help`
flag for each (sub)command.  
Flags and their default values may differ across operating systems.

#### Client and server commands

Most subcommands are client commands that connect to the file system service daemon.  
If the service daemon is not running, and a client command did not specify the `-api-server`
flag, the client process will try to invoke the `fs daemon` command automatically.  
That process will periodically check if it's no longer needed and exit if that's the case. To
set the idle check interval for that process, client commands can provide the `-api-exit-after`
flag.

### cmd/build

`cmd/build` attempts to set up a process environment before invoking the `go` tool to build
`cmd/fs`.  
`cmd/fs` has the same requirements as [cgofuse](https://github.com/winfsp/cgofuse#how-to-build),
so these must be installed for `cmd/build` to succeed.  

The build command is typically executed via `go run ./cmd/build` from the root of the source
directory.  
Different build modes may be specified via the `-mode` flag. By default, `cmd/build` will try to
build in "release" mode, which produces a smaller binary.  

---

If you'd like to build `cmd/fs` manually, you can invoke `go build` on the `cmd/fs` command,
but must make sure the compiler used by CGO can find the FUSE library headers.  

On Windows with the default WinFSP install path, you can set the `CPATH` environment variable
like this:

```pwsh
$ENV:CPATH = $(Join-Path (${ENV:ProgramFiles(x86)} ?? ${ENV:ProgramFiles}) "WinFsp" "inc" "fuse")
go build .\cmd\fs
```

POSIX systems are expected to have libraries in a standard path,
but may be specified in a similar way:

```sh
CPATH=/path/to/libfuse go build ./cmd/fs
```

### cmd/fs

`cmd/fs` is the main command used to interact with file system services.  

#### mount | unmount

Generally, you'll want to mount a system using this pattern:  
`fs $hostAPI $guestAPI $mountPoint`  

And for unmounting:  
`fs unmount $mountPoint` or `fs unmount -all`

For both `mount` and `unmount`, multiple mount points may be specified in a single invocation.

On POSIX systems, valid mount points are typically (existing) directories.  
E.g. `fs mount fuse ipfs /mnt/ipfs`.  
On NT systems, valid mount points may be drive letters `X:`, non-existing paths `C:\mountpoint`,
or UNC location `\\Server\Share`.  
E.g. `fs mount fuse ipfs I: C:\ipfs \\localhost\ipfs`

Each pair of host and guest APIs may have its own set of command line flags and constraints that
should be outlined their `-help` text if applicable.

#### daemon

The file system service daemon is typically summoned automatically by client commands,
but may be invoked separately via the `fs daemon` command.  
The `-api-server=$multiaddr` flag may be provided to the daemon to specify
which address(es) it will listen on.  
The same flag may be provided to client commands to specify which service address to use.

#### shutdown

The daemon can be requested to stop via `fs shutdown`.  
By default a "patient" request is sent, which prevents new connections from being established,
and closes existing connections after they're considered idle.  
When all connections are closed, any active mounts are unmounted and the process exits.

Alternate shutdown dispositions may be provided via the `-level` flag.  
Such as "short" which will close existing connection after some short time (regardless of if
they're idle or not).  
And "immediate" which closes existing connections immediately.  

Note that `shutdown` only requests a shutdown, it does not wait for the shutdown process to
finish.

## 9P API
The 9P API is not yet stable or well documented, but is the primary interface used by the `fs`
client commands.  

The `fs daemon` is a 9P file system server which listens on the multiaddrs provided by
the `-api-server` flag, which is where the API is to be exposed.  

![file system](assets/filesystem.svg)
(\*The SVG above contains hyperlinks to schemas for the JSON being referred to.)  

The `-verbose` flag may be provided when invoking commands. This prints out the 9P messages sent
and received from both the client and server. This effectively traces the protocol which is
useful for understanding the expected sequence, and for debugging.

---

External processes may choose to interact with the API by connecting to it and sending messages
that adhere to the [9P2000.L
specification](https://github.com/chaos/diod/blob/master/protocol.md).  
Common external clients are the operating system itself. Such as NT, Linux, Plan 9, et
al.  

Here is one such example of replicating the `fs mount` and `fs unmount` commands via a POSIX
shell.

```sh
# Start the daemon process in the background.
fs daemon -api-server /ip4/192.168.1.40/tcp/564 &
# Mount the 9P API
mount -t 9p 192.168.1.40 /mnt/9 -o "trans=tcp,port=564"
# Create the mount point's metadata path; populate the mount metdata in "field mode" via a "here document".
# This is equivalent to calling `fs mount fuse pinfs -ipfs=/ip4/192.168.1.40/tcp/5001 /mnt/ipfs`.
mkdir -p /mnt/9/mounts/FUSE/PinFS; cat << EOF > /mnt/9/mounts/FUSE/PinFS/mountpoint
host.point /mnt/ipfs
guest.apiMaddr /ip4/192.168.1.40/tcp/5001
EOF
# Mount point metadata is formated as JSON when read back.
# We can back up this virtual file to a real on-disk file.
cp /mnt/9/mount/FUSE/PinFs/mountpoint ~/mountpoint.json
# Removing the metadata file is equivalent to calling `fs unmount /mnt/ipfs`.
rm /mnt/9/mount/FUSE/PinFs/mountpoint
# When directories are empty, under certain circumstances they will be unlinked automatically.
# In this case we need to create the path again.
# Previously we created the file by writing field data line by line, but JSON is also accepted.
# So this too is equivalent to calling `fs mount fuse pinfs -ipfs=/ip4/192.168.1.40/tcp/5001 /mnt/ipfs`.
mkdir -p /mnt/9/mounts/FUSE/PinFS; cp ~/mountpoint.json /mnt/9/mounts/FUSE/Pinfs/same-one.json
```

---

In the future, file systems will likely expose their own documentation as a readable file,
similar to how commands contain their own help text.  
I.e. `cat /mounts/manual`, `cat /mounts/FUSE/PinFS/manual`, etc.

<!-- vi: set textwidth=96: -->
