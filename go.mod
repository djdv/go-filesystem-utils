module github.com/djdv/go-filesystem-utils

go 1.17

require (
	github.com/adrg/xdg v0.3.3
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/fatih/camelcase v1.0.0
	github.com/ipfs/go-ipfs-cmds v0.6.0
	github.com/kardianos/service v0.0.0-00010101000000-000000000000
	github.com/multiformats/go-multiaddr v0.3.1
	golang.org/x/sys v0.0.0-20210303074136-134d130e1a04
)

replace (
	github.com/ipfs/go-ipfs-cmds => github.com/djdv/go-ipfs-cmds v0.0.0-20210504182537-92cf96be03f0
	github.com/kardianos/service => github.com/djdv/service v1.2.1-0.20210722163916-5625a2cdd715
)

require (
	github.com/Kubuxu/go-os-helper v0.0.1 // indirect
	github.com/crackcomm/go-gitignore v0.0.0-20170627025303-887ab5e44cc3 // indirect
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/ipfs/go-cid v0.0.7 // indirect
	github.com/ipfs/go-ipfs-files v0.0.8 // indirect
	github.com/ipfs/go-log v1.0.4 // indirect
	github.com/ipfs/go-log/v2 v2.0.5 // indirect
	github.com/minio/blake2b-simd v0.0.0-20160723061019-3f5f724cb5b1 // indirect
	github.com/minio/sha256-simd v0.1.1-0.20190913151208-6de447530771 // indirect
	github.com/mr-tron/base58 v1.1.3 // indirect
	github.com/multiformats/go-base32 v0.0.3 // indirect
	github.com/multiformats/go-base36 v0.1.0 // indirect
	github.com/multiformats/go-multibase v0.0.3 // indirect
	github.com/multiformats/go-multihash v0.0.14 // indirect
	github.com/multiformats/go-varint v0.0.6 // indirect
	github.com/opentracing/opentracing-go v1.1.0 // indirect
	github.com/rs/cors v1.7.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/texttheater/golang-levenshtein v0.0.0-20180516184445-d188e65d659e // indirect
	go.uber.org/atomic v1.6.0 // indirect
	go.uber.org/multierr v1.5.0 // indirect
	go.uber.org/zap v1.14.1 // indirect
	golang.org/x/crypto v0.0.0-20200115085410-6d4e4cb37c7d // indirect
)
