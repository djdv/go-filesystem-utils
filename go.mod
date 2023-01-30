module github.com/djdv/go-filesystem-utils

go 1.19

require (
	github.com/adrg/xdg v0.4.0
	github.com/hashicorp/golang-lru/v2 v2.0.2
	github.com/hugelgupf/p9 v0.0.0-00010101000000-000000000000
	github.com/ipfs/boxo v0.10.2-0.20230629143123-2d3edc552442
	github.com/ipfs/go-cid v0.4.1
	github.com/ipfs/go-ipfs-cmds v0.9.0
	github.com/ipfs/go-ipfs-http-client v0.5.1-0.20230418133148-ae996cbe5a91
	github.com/ipfs/go-ipld-cbor v0.0.6
	github.com/ipfs/go-ipld-format v0.5.0
	github.com/ipfs/kubo v0.21.0
	github.com/jaevor/go-nanoid v1.3.0
	github.com/multiformats/go-multiaddr v0.9.0
	github.com/multiformats/go-multiaddr-dns v0.3.1
	github.com/multiformats/go-multibase v0.2.0
	github.com/u-root/uio v0.0.0-20220204230159-dac05f7d2cb4
	github.com/winfsp/cgofuse v1.5.1-0.20230130140708-f87f5db493b5
	golang.org/x/exp v0.0.0-20230321023759-10a507213a29
	golang.org/x/sys v0.7.0
)

require (
	github.com/alecthomas/units v0.0.0-20211218093645-b94a6e3cc137 // indirect
	github.com/crackcomm/go-gitignore v0.0.0-20170627025303-887ab5e44cc3 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.2.0 // indirect
	github.com/go-logr/logr v1.2.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/ipfs/bbloom v0.0.4 // indirect
	github.com/ipfs/go-bitfield v1.1.0 // indirect
	github.com/ipfs/go-block-format v0.1.2 // indirect
	github.com/ipfs/go-datastore v0.6.0 // indirect
	github.com/ipfs/go-ipfs-util v0.0.3 // indirect
	github.com/ipfs/go-ipld-legacy v0.2.1 // indirect
	github.com/ipfs/go-log v1.0.5 // indirect
	github.com/ipfs/go-log/v2 v2.5.1 // indirect
	github.com/ipfs/go-metrics-interface v0.0.1 // indirect
	github.com/ipld/go-codec-dagpb v1.6.0 // indirect
	github.com/ipld/go-ipld-prime v0.20.0 // indirect
	github.com/jbenet/goprocess v0.1.4 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/libp2p/go-buffer-pool v0.1.0 // indirect
	github.com/libp2p/go-libp2p v0.27.7 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/miekg/dns v1.1.54 // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/multiformats/go-base32 v0.1.0 // indirect
	github.com/multiformats/go-base36 v0.2.0 // indirect
	github.com/multiformats/go-multicodec v0.9.0 // indirect
	github.com/multiformats/go-multihash v0.2.3 // indirect
	github.com/multiformats/go-multistream v0.4.1 // indirect
	github.com/multiformats/go-varint v0.0.7 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/polydawn/refmt v0.89.0 // indirect
	github.com/rs/cors v1.7.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/whyrusleeping/cbor-gen v0.0.0-20230126041949-52956bd4c9aa // indirect
	go.opentelemetry.io/otel v1.16.0 // indirect
	go.opentelemetry.io/otel/metric v1.16.0 // indirect
	go.opentelemetry.io/otel/trace v1.16.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.24.0 // indirect
	golang.org/x/crypto v0.10.0 // indirect
	golang.org/x/mod v0.10.0 // indirect
	golang.org/x/net v0.10.0 // indirect
	golang.org/x/sync v0.2.0 // indirect
	golang.org/x/sys v0.9.0 // indirect
	golang.org/x/tools v0.9.1 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	google.golang.org/protobuf v1.30.0 // indirect
	lukechampine.com/blake3 v1.2.1 // indirect
)

// FIXME: Ideally we remove this replace directive when upstream merges
// if that doesn't happen before end of review, we'll have to fork.
replace github.com/hugelgupf/p9 => github.com/djdv/p9 v0.2.1-0.20221024045104-6b0c9ca47f00
