module github.com/djdv/go-filesystem-utils

go 1.19

require (
	github.com/adrg/xdg v0.4.0
	github.com/hugelgupf/p9 v0.0.0-00010101000000-000000000000
	github.com/ipfs/go-cid v0.2.0
	github.com/ipfs/go-ipfs-files v0.1.1
	github.com/ipfs/go-ipfs-http-client v0.4.0
	github.com/ipfs/go-ipld-cbor v0.0.5
	github.com/ipfs/go-ipld-format v0.4.0
	github.com/ipfs/go-merkledag v0.6.0
	github.com/ipfs/go-path v0.3.0
	github.com/ipfs/go-unixfs v0.4.0
	github.com/ipfs/interface-go-ipfs-core v0.7.0
	github.com/ipfs/kubo v0.15.0
	github.com/jaevor/go-nanoid v1.3.0
	github.com/multiformats/go-multiaddr v0.6.0
	github.com/multiformats/go-multiaddr-dns v0.3.1
	github.com/u-root/uio v0.0.0-20220204230159-dac05f7d2cb4
	github.com/winfsp/cgofuse v1.5.1-0.20221118130120-84c0898ad2e0
	golang.org/x/exp v0.0.0-20220921023135-46d9e7742f1e
)

require (
	github.com/btcsuite/btcd/btcec/v2 v2.2.0 // indirect
	github.com/crackcomm/go-gitignore v0.0.0-20170627025303-887ab5e44cc3 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/ipfs/bbloom v0.0.4 // indirect
	github.com/ipfs/go-block-format v0.0.3 // indirect
	github.com/ipfs/go-blockservice v0.4.0 // indirect
	github.com/ipfs/go-datastore v0.5.1 // indirect
	github.com/ipfs/go-ipfs-blockstore v1.2.0 // indirect
	github.com/ipfs/go-ipfs-cmds v0.8.1 // indirect
	github.com/ipfs/go-ipfs-ds-help v1.1.0 // indirect
	github.com/ipfs/go-ipfs-exchange-interface v0.2.0 // indirect
	github.com/ipfs/go-ipfs-util v0.0.2 // indirect
	github.com/ipfs/go-ipld-legacy v0.1.1 // indirect
	github.com/ipfs/go-log v1.0.5 // indirect
	github.com/ipfs/go-log/v2 v2.5.1 // indirect
	github.com/ipfs/go-metrics-interface v0.0.1 // indirect
	github.com/ipfs/go-verifcid v0.0.2 // indirect
	github.com/ipld/go-codec-dagpb v1.4.1 // indirect
	github.com/ipld/go-ipld-prime v0.17.0 // indirect
	github.com/jbenet/goprocess v0.1.4 // indirect
	github.com/klauspost/cpuid/v2 v2.0.14 // indirect
	github.com/libp2p/go-buffer-pool v0.1.0 // indirect
	github.com/libp2p/go-libp2p-core v0.19.1 // indirect
	github.com/libp2p/go-libp2p-resource-manager v0.5.3 // indirect
	github.com/libp2p/go-openssl v0.0.7 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/miekg/dns v1.1.50 // indirect
	github.com/minio/sha256-simd v1.0.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/multiformats/go-base32 v0.0.4 // indirect
	github.com/multiformats/go-base36 v0.1.0 // indirect
	github.com/multiformats/go-multibase v0.1.1 // indirect
	github.com/multiformats/go-multicodec v0.5.0 // indirect
	github.com/multiformats/go-multihash v0.2.1 // indirect
	github.com/multiformats/go-varint v0.0.6 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pbnjay/memory v0.0.0-20210728143218-7b4eea64cf58 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/polydawn/refmt v0.0.0-20201211092308-30ac6d18308e // indirect
	github.com/rs/cors v1.7.0 // indirect
	github.com/spacemonkeygo/spacelog v0.0.0-20180420211403-2296661a0572 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/whyrusleeping/cbor-gen v0.0.0-20210219115102-f37d292932f2 // indirect
	go.opentelemetry.io/otel v1.7.0 // indirect
	go.opentelemetry.io/otel/trace v1.7.0 // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	go.uber.org/zap v1.21.0 // indirect
	golang.org/x/crypto v0.0.0-20220525230936-793ad666bf5e // indirect
	golang.org/x/mod v0.6.0-dev.0.20220419223038-86c51ed26bb4 // indirect
	golang.org/x/net v0.0.0-20220722155237-a158d28d115b // indirect
	golang.org/x/sys v0.0.0-20220722155257-8c9f86f7a55f // indirect
	golang.org/x/tools v0.1.12 // indirect
	golang.org/x/xerrors v0.0.0-20220609144429-65e65417b02f // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	lukechampine.com/blake3 v1.1.7 // indirect
)

// FIXME: Ideally we remove this replace directive when upstream merges
// if that doesn't happen before end of review, we'll have to fork.
replace github.com/hugelgupf/p9 => github.com/djdv/p9 v0.2.1-0.20221024045104-6b0c9ca47f00
