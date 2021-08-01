package filesystem

import (
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

type Stat struct {
	Type      coreiface.FileType
	Size      uint64
	BlockSize uint64
	Blocks    uint64
	/* TODO: UFS 2 when it's done
	ATimeNano int64
	MTimeNano int64
	CTimeNano int64 */
}

var StatRequestAll = StatRequest{
	Type: true, Size: true, Blocks: true,
}

type StatRequest struct {
	Type   bool
	Size   bool
	Blocks bool
	/* TODO: UFS 2 when it's done
	ATime       bool
	MTime       bool
	CTime       bool
	*/
}
