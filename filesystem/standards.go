package filesystem

import (
	"errors"
	"fmt"

	"github.com/multiformats/go-multiaddr"
)

type (
	protocolCode int32 // multiaddr identification tokens

	API protocolCode // represents a particular host API (e.g. 9P, Fuse, et al.)
	ID  protocolCode // represents a particular file system implementation (e.g. IPFS, IPNS, et al.)
)

//go:generate stringer -type=API,ID -linecomment -output standards_string.go
const (
	_ API = API(^uint32(0)>>1) - iota

	// NOTE: values are currently experimental/unstable
	// For now, we use the max value supported, decrementing; acting as our private range of protocol values
	// (internally: go-multiaddr/d18c05e0e1635f8941c93f266beecaedd4245b9f/varint.go:10)
	//
	// Stringer values correspond to a namespace registered within the Multiaddr library.
	Fuse          // fuse
	Plan9Protocol // 9p

	_     ID = iota
	IPFS     // ipfs
	IPNS     // ipns
	Files    // file
	PinFS    // pinfs
	KeyFS    // keyfs

	// Existing Multicodec standards:
	// TODO [review]: this protocol may be defined in another package
	// we should use it or add it if it doesn't exist.
	// (^go-multiaddr itself should register `/path`?)
	// For now we use the standard multicodec value with a non-standard implementation.
	PathProtocol API = 0x2f // path

)

func init() {
	var err error
	if err = registerStandardProtocols(); err != nil {
		panic(err)
	}
	if err = registerAPIProtocols(Fuse, Plan9Protocol); err != nil {
		panic(err)
	}
	registerSystemIDs(IPFS, IPNS, Files, PinFS, KeyFS)
}

var ErrUnexpectedID = errors.New("unexpected ID value")

func registerStandardProtocols() error {
	return multiaddr.AddProtocol(multiaddr.Protocol{
		Name:  PathProtocol.String(),
		Code:  int(PathProtocol),
		VCode: multiaddr.CodeToVarint(int(PathProtocol)),
		Size:  multiaddr.LengthPrefixedVarSize,
		Path:  true,
		Transcoder: multiaddr.NewTranscoderFromFunctions(
			func(s string) ([]byte, error) { return []byte(s), nil },
			func(b []byte) (string, error) { return string(b), nil },
			nil),
	})
}

// registers API names within the mutliaddr hierarchy
func registerAPIProtocols(apis ...API) (err error) {
	for _, api := range apis {
		err = multiaddr.AddProtocol(multiaddr.Protocol{
			Name:  api.String(),
			Code:  int(api),
			VCode: multiaddr.CodeToVarint(int(api)),
			//Size:  32, // TODO: const? sizeof (API)
			Size: multiaddr.LengthPrefixedVarSize,
			Transcoder: multiaddr.NewTranscoderFromFunctions(
				apiStringToBytes, nodeAPIBytesToString,
				nil),
		})
		if err != nil {
			return
		}
	}
	return
}

var (
	stringToID = make(map[string]ID)
	idToString = make(map[ID]string)
)

// populates the StringToID name registry
func registerSystemIDs(ids ...ID) {
	for _, id := range ids {
		stringToID[id.String()] = id
		idToString[id] = id.String()
	}
}

func apiStringToBytes(systemName string) (buf []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprintf("%s", r))
		}
	}()

	id, ok := stringToID[systemName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnexpectedID, systemName)
	}

	return multiaddr.CodeToVarint(int(id)), nil
}

func nodeAPIBytesToString(buffer []byte) (value string, err error) {
	var id int
	id, _, err = multiaddr.ReadVarintCode(buffer)
	if err != nil {
		err = fmt.Errorf("could not decode node-API varint: %w", err)
		return
	}

	var ok bool
	if value, ok = idToString[ID(id)]; !ok {
		err = fmt.Errorf("%w: %#x", ErrUnexpectedID, id)
	}

	return
}
