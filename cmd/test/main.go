package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem/pinfs"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
)

func main() {
	log.SetFlags(log.Lshortfile)

	// TODO [report this][t-notes]: When this maddr is not correct, CoreAPI requests return
	//`command not found`
	// this is confusing and should say something at least when the connection isn't dialable.
	// actual API compliance can be done by clients if needed,
	// but the connection error should come back to us in this case
	//ipfsMaddr := multiaddr.StringCast("/ip4/127.0.0.1/tcp/8080")
	ipfsMaddr := multiaddr.StringCast("/ip4/127.0.0.1/tcp/5001")

	/*
		testEncodingMaddr := fscmds.Multiaddr{ipfsMaddr}

		bytes, err := json.Marshal(testEncodingMaddr)
		if err != nil {
			panic(err)
		}

		fmt.Println((string)(bytes))

		var addr fscmds.Multiaddr
		err = json.Unmarshal(bytes, &addr)
		if err != nil {
			panic(err)
		}
		fmt.Println(addr)

		return
	*/

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

	entries, err := fs.ReadDir(fsi, ".")
	if !errors.Is(err, io.EOF) {
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

	log.Println("close:", aboutFile.Close())
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
