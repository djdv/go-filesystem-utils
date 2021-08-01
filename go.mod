module github.com/djdv/go-filesystem-utils

go 1.16

require (
	bazil.org/fuse v0.0.0-20200524192727-fb710f7dfd05
	github.com/adrg/xdg v0.3.3
	github.com/billziss-gh/cgofuse v1.5.0
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/fatih/camelcase v1.0.0
	github.com/ipfs/go-cid v0.0.7
	github.com/ipfs/go-detect-race v0.0.1
	github.com/ipfs/go-ipfs v0.9.1
	github.com/ipfs/go-ipfs-chunker v0.0.5
	github.com/ipfs/go-ipfs-cmds v0.6.0
	github.com/ipfs/go-ipfs-config v0.14.0
	github.com/ipfs/go-ipfs-files v0.0.8
	github.com/ipfs/go-ipfs-http-client v0.1.0
	github.com/ipfs/go-ipfs-util v0.0.2
	github.com/ipfs/go-ipld-cbor v0.0.5
	github.com/ipfs/go-ipld-format v0.2.0
	github.com/ipfs/go-log v1.0.5
	github.com/ipfs/go-merkledag v0.3.2
	github.com/ipfs/go-mfs v0.1.2
	github.com/ipfs/go-namesys v0.3.0
	github.com/ipfs/go-path v0.0.9
	github.com/ipfs/go-unixfs v0.2.6
	github.com/ipfs/interface-go-ipfs-core v0.4.0
	github.com/kardianos/service v0.0.0-00010101000000-000000000000
	github.com/libp2p/go-libp2p-core v0.8.5
	github.com/libp2p/go-libp2p-testing v0.4.0
	github.com/multiformats/go-multiaddr v0.3.3
	github.com/multiformats/go-multiaddr-dns v0.3.1
	github.com/olekukonko/tablewriter v0.0.5
	golang.org/x/sys v0.0.0-20210511113859-b0526f3d8744
)

replace (
	github.com/ipfs/go-ipfs-cmds => github.com/djdv/go-ipfs-cmds v0.0.0-20210504182537-92cf96be03f0
	github.com/kardianos/service => github.com/djdv/service v1.2.1-0.20210722163916-5625a2cdd715
)

// TODO: 0.2.6 introduced breaking changes; `ipfs/go-ipfs` and `ipfs/go-mfs` are invalidated by this - remove this when resolved upstream
replace github.com/ipfs/go-unixfs => github.com/ipfs/go-unixfs v0.2.5
