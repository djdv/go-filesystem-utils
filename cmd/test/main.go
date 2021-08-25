package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"testing/fstest"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/filesystem/pinfs"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
)

// TODO: we need to turn parts of this into proper Go tests.
// (grab the daemon spawning + pin setup tests from the old module branch)

func main() {
	log.SetFlags(log.Lshortfile)
	ipfsMaddr := multiaddr.StringCast("/ip4/127.0.0.1/tcp/5001")

	testEncodingMaddr := formats.Multiaddr{ipfsMaddr}

	bytes, err := json.Marshal(testEncodingMaddr)
	if err != nil {
		panic(err)
	}

	fmt.Println((string)(bytes))

	var addr formats.Multiaddr
	err = json.Unmarshal(bytes, &addr)
	if err != nil {
		panic(err)
	}
	fmt.Println(addr)

	coreAPI, err := ipfsClient(ipfsMaddr)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	//fsi := ipfs.NewInterface(context.Background(), coreAPI, filesystem.IPFS)
	fsi := pinfs.NewInterface(ctx, coreAPI)

	pinChan, err := coreAPI.Pin().Ls(ctx, coreoptions.Pin.Ls.Recursive())
	if err != nil {
		log.Fatal(err)
	}
	for pin := range pinChan {
		fmt.Println("pin from API:", pin.Path())
	}
	fmt.Println("pins from API done")

	entries, err := fs.ReadDir(fsi, ".")
	if err != nil {
		log.Fatal(err)
	}

	for _, ent := range entries {
		fmt.Println("ent from FS:", ent.Name())
	}

	const aboutName = "QmPZ9gcCEpqKTo6aq61g2nXGUhM4iCL3ewB6LDXZCtioEB"

	aboutFile, err := fsi.Open(aboutName)
	if err != nil {
		log.Fatal(err)
	}

	const aboutSize = 1681
	aboutBuffer := make([]byte, aboutSize)
	read, err := aboutFile.Read(aboutBuffer)
	log.Printf("read: %d/%d, err: %v\n", read, aboutSize, err)
	log.Printf("%s\n", aboutBuffer)

	if err := aboutFile.Close(); err != nil {
		log.Fatal("close:", err)
	}

	//if err := fstest.TestFS(fsi); err != nil {
	if err := fstest.TestFS(fsi, "QmQPeNsJPyVWPFDVHb77w8G42Fvo15z4bG2X8D2GhfbSXc"); err != nil {
		log.Fatal(err)
	}
}

func ipfsClient(apiMaddr multiaddr.Multiaddr) (coreiface.CoreAPI, error) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFunc()
	resolvedMaddr, err := resolveMaddr(ctx, apiMaddr)
	if err != nil {
		return nil, err
	}
	return httpapi.NewApi(resolvedMaddr)
}

func resolveMaddr(ctx context.Context, addr multiaddr.Multiaddr) (multiaddr.Multiaddr, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, 10*time.Second)
	defer cancelFunc()

	addrs, err := madns.DefaultResolver.Resolve(ctx, addr)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		return nil, errors.New("non-resolvable API endpoint")
	}

	return addrs[0], nil
}
