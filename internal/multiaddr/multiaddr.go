package multiaddr

import (
	"encoding/json"

	"github.com/multiformats/go-multiaddr"
)

// Multiaddr wraps the reference Multiaddr library
// adding deserialization support.
type Multiaddr struct{ multiaddr.Multiaddr }

func (ma *Multiaddr) MarshalBinary() ([]byte, error) {
	if maddr := ma.Multiaddr; maddr != nil {
		return maddr.MarshalBinary()
	}
	return nil, nil
}

func (ma *Multiaddr) UnmarshalBinary(b []byte) error {
	maddr, err := multiaddr.NewMultiaddrBytes(b)
	if err != nil {
		return err
	}
	ma.Multiaddr = maddr
	return nil
}

func (ma *Multiaddr) MarshalText() ([]byte, error) {
	if maddr := ma.Multiaddr; maddr != nil {
		return maddr.MarshalText()
	}
	return []byte{}, nil
}

func (ma *Multiaddr) UnmarshalText(b []byte) error {
	maddr, err := multiaddr.NewMultiaddr(string(b))
	if err != nil {
		return err
	}
	ma.Multiaddr = maddr
	return nil
}

func (ma *Multiaddr) MarshalJSON() ([]byte, error) {
	if maddr := ma.Multiaddr; maddr != nil {
		return maddr.MarshalJSON()
	}
	return []byte("null"), nil
}

func (ma *Multiaddr) UnmarshalJSON(b []byte) error {
	var maddrString string
	if err := json.Unmarshal(b, &maddrString); err != nil {
		return err
	}
	maddr, err := multiaddr.NewMultiaddr(maddrString)
	if err != nil {
		return err
	}
	ma.Multiaddr = maddr
	return nil
}
