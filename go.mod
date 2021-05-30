module github.com/djdv/go-filesystem-utils

go 1.16

require (
	github.com/adrg/xdg v0.3.3
	github.com/ipfs/go-ipfs-cmds v0.6.0
	github.com/kardianos/service v0.0.0-00010101000000-000000000000
	github.com/multiformats/go-multiaddr v0.3.1
	github.com/multiformats/go-multiaddr-dns v0.3.1
	golang.org/x/sys v0.0.0-20210303074136-134d130e1a04
)

replace (
	github.com/ipfs/go-ipfs-cmds => github.com/djdv/go-ipfs-cmds v0.0.0-20210504182537-92cf96be03f0
	github.com/kardianos/service => github.com/djdv/service v1.2.1-0.20210530025522-eb90fecf3353
)
