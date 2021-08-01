package filesystem_test

import (
	"fmt"

	"github.com/ipfs/go-ipfs/filesystem"
	"github.com/multiformats/go-multiaddr"
)

func Example() {
	for _, api := range []filesystem.API{
		filesystem.Fuse,
		filesystem.Plan9Protocol,
	} {
		for _, id := range []filesystem.ID{
			filesystem.IPFS,
			filesystem.IPNS,
		} {
			header, _ := multiaddr.NewComponent(api.String(), id.String())
			remainder, _ := multiaddr.NewComponent(filesystem.PathProtocol.String(),
				fmt.Sprintf("/mnt/{Example-%s-%s}", api, id))
			maddr := header.Encapsulate(remainder)
			fmt.Println(maddr)
		}
	}
	// Output:
	// /fuse/ipfs/path/mnt/{Example-fuse-ipfs}
	// /fuse/ipns/path/mnt/{Example-fuse-ipns}
	// /9p/ipfs/path/mnt/{Example-9p-ipfs}
	// /9p/ipns/path/mnt/{Example-9p-ipns}
}
