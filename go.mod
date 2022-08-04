module github.com/djdv/go-filesystem-utils

go 1.19

require (
	github.com/hugelgupf/p9 v0.2.0
	github.com/multiformats/go-multiaddr v0.6.0
	github.com/u-root/uio v0.0.0-20220204230159-dac05f7d2cb4
	golang.org/x/exp v0.0.0-20220706164943-b4a6d9510983
)

require (
	github.com/ipfs/go-cid v0.0.7 // indirect
	github.com/minio/blake2b-simd v0.0.0-20160723061019-3f5f724cb5b1 // indirect
	github.com/minio/sha256-simd v0.1.1-0.20190913151208-6de447530771 // indirect
	github.com/mr-tron/base58 v1.1.3 // indirect
	github.com/multiformats/go-base32 v0.0.3 // indirect
	github.com/multiformats/go-base36 v0.1.0 // indirect
	github.com/multiformats/go-multibase v0.0.3 // indirect
	github.com/multiformats/go-multihash v0.0.14 // indirect
	github.com/multiformats/go-varint v0.0.6 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9 // indirect
	golang.org/x/sys v0.0.0-20220114195835-da31bd327af9 // indirect
)

// FIXME: Ideally we remove this replace directive when upstream merges
// if that doesn't happen before end of review, we'll have to fork.
replace github.com/hugelgupf/p9 => github.com/djdv/p9 v0.2.1-0.20220727204224-9a076d69a162
