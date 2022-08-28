package files

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/multiformats/go-multiaddr"
)

type ipfsAPIMultiaddr struct{ multiaddr.Multiaddr }

func (maddr *ipfsAPIMultiaddr) UnmarshalBinary(b []byte) (err error) {
	maddr.Multiaddr, err = multiaddr.NewMultiaddrBytes(b)
	return
}

func (maddr *ipfsAPIMultiaddr) UnmarshalText(b []byte) (err error) {
	maddr.Multiaddr, err = multiaddr.NewMultiaddr(string(b))
	return
}

func (maddr *ipfsAPIMultiaddr) UnmarshalJSON(b []byte) (err error) {
	if len(b) < 2 || bytes.Equal(b, []byte("{}")) {
		return fmt.Errorf("response was empty or short: `%v`", b)
	}
	if bytes.Equal(b, []byte("null")) {
		return
	}
	var maddrStr string
	if err = json.Unmarshal(b, &maddrStr); err != nil {
		return
	}
	maddr.Multiaddr, err = multiaddr.NewMultiaddr(maddrStr)
	return
}
