//go:build !windows && nofuse
// +build !windows,nofuse

package fscmds

const (
	MountTagline          = "Command disabled in this build"
	mountDescWhatAndWhere = `
This version of ipfs is compiled without fuse support, which is required
for mounting. If you'd like to be able to mount, please use a version of
ipfs compiled with fuse.
For the latest instructions, please check the project's repository:
  http://github.com/ipfs/go-ipfs
`
	mountDescExample = `None`
)
