package ipfs

import (
	"encoding/json"

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

func (mc *multiaddrContainer) UnmarshalJSON(b []byte) error {
	var mcStr string
	if err := json.Unmarshal(b, &mcStr); err != nil {
		return err
	}
	maddr, err := multiaddr.NewMultiaddr(mcStr)
	if err != nil {
		return err
	}
	mc.Multiaddr = maddr
	return nil
}
