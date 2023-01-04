package ipfs

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/multiformats/go-multiaddr"
)

type multiaddrContainer struct{ multiaddr.Multiaddr }

func (mc *multiaddrContainer) MarshalBinary() ([]byte, error) {
	if maddr := mc.Multiaddr; maddr != nil {
		return maddr.MarshalBinary()
	}
	return []byte{}, nil
}

func (mc *multiaddrContainer) UnmarshalBinary(b []byte) (err error) {
	mc.Multiaddr, err = multiaddr.NewMultiaddrBytes(b)
	return
}

func (mc *multiaddrContainer) MarshalText() ([]byte, error) {
	if maddr := mc.Multiaddr; maddr != nil {
		return maddr.MarshalText()
	}
	return []byte{}, nil
}

func (mc *multiaddrContainer) UnmarshalText(b []byte) (err error) {
	mc.Multiaddr, err = multiaddr.NewMultiaddr(string(b))
	return
}

func (mc *multiaddrContainer) MarshalJSON() ([]byte, error) {
	if maddr := mc.Multiaddr; maddr != nil {
		return maddr.MarshalJSON()
	}
	return []byte("null"), nil
}

func (mc *multiaddrContainer) UnmarshalJSON(b []byte) (err error) {
	if len(b) < 2 || bytes.Equal(b, []byte("{}")) {
		return fmt.Errorf("response was empty or short: `%v`", b)
	}
	if bytes.Equal(b, []byte("null")) {
		return
	}
	var mcStr string
	if err = json.Unmarshal(b, &mcStr); err != nil {
		return
	}
	mc.Multiaddr, err = multiaddr.NewMultiaddr(mcStr)
	return
}
