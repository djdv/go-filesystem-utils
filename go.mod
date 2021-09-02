module github.com/djdv/go-filesystem-utils

go 1.16

require (
	github.com/adrg/xdg v0.3.3
	github.com/billziss-gh/cgofuse v1.5.0
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/fatih/camelcase v1.0.0
	github.com/ipfs/go-ipfs-cmds v0.6.0
	github.com/ipfs/go-ipfs-files v0.0.8
	github.com/ipfs/go-ipld-cbor v0.0.5
	github.com/ipfs/go-ipld-format v0.2.0
	github.com/ipfs/go-log v1.0.5
	github.com/ipfs/go-merkledag v0.4.0
	github.com/ipfs/go-unixfs v0.2.6
	github.com/ipfs/interface-go-ipfs-core v0.4.0
	github.com/kardianos/service v0.0.0-00010101000000-000000000000
	github.com/multiformats/go-multiaddr v0.3.1
	golang.org/x/sys v0.0.0-20210514084401-e8d321eab015
)

replace (
	github.com/ipfs/go-ipfs-cmds => github.com/djdv/go-ipfs-cmds v0.0.0-20210504182537-92cf96be03f0
	github.com/kardianos/service => github.com/djdv/service v1.2.1-0.20210722163916-5625a2cdd715
)
