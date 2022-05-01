package cmdsmaddr

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/multiformats/go-multiaddr"
)

// Encapsulated is an implementation of the Multiaddr interface.
// This is (unfortunately) necessary for (un)marshaling
// since there is no (public) standard implementation.
// See: https://github.com/multiformats/go-multiaddr/issues/100
type Encapsulated struct{ multiaddr.Multiaddr }

func (maddr Encapsulated) MarshalBinary() ([]byte, error) {
	return maddr.Multiaddr.Bytes(), nil
}

func (maddr *Encapsulated) UnmarshalBinary(b []byte) error {
	decoded, err := multiaddr.NewMultiaddrBytes(b)
	if err != nil {
		return err
	}
	maddr.Multiaddr = decoded
	return nil
}

func (maddr Encapsulated) MarshalText() ([]byte, error) {
	return []byte(maddr.Multiaddr.String()), nil
}

func (maddr *Encapsulated) UnmarshalText(b []byte) error {
	decoded, err := multiaddr.NewMultiaddr(string(b))
	if err != nil {
		return err
	}
	maddr.Multiaddr = decoded
	return nil
}

func (maddr Encapsulated) MarshalJSON() ([]byte, error) {
	if maddr.Multiaddr == nil {
		return nil, fmt.Errorf("response's Request field must be populated")
	}
	return json.Marshal(maddr.Multiaddr.Bytes())
}

func (maddr *Encapsulated) UnmarshalJSON(b []byte) error {
	if len(b) < 2 || bytes.Equal(b, []byte("{}")) {
		return fmt.Errorf("response was empty or short: `%v`", b)
	}
	if bytes.Equal(b, []byte("null")) {
		return nil
	}

	angryBytes := make([]byte, 0)
	if err := json.Unmarshal(b, &angryBytes); err != nil {
		return err
	}
	maddrBytes, err := multiaddr.NewMultiaddrBytes(angryBytes)
	if err != nil {
		return err
	}

	maddr.Multiaddr = maddrBytes
	return nil
}

func (maddr *Encapsulated) Equal(arg multiaddr.Multiaddr) bool {
	return maddr.Multiaddr.Equal(arg)
}
func (maddr *Encapsulated) Bytes() []byte  { return maddr.Multiaddr.Bytes() }
func (maddr *Encapsulated) String() string { return maddr.Multiaddr.String() }
func (maddr *Encapsulated) Protocols() []multiaddr.Protocol {
	return maddr.Multiaddr.Protocols()
}
func (maddr *Encapsulated) Encapsulate(arg multiaddr.Multiaddr) multiaddr.Multiaddr {
	return maddr.Multiaddr.Encapsulate(arg)
}

func (maddr *Encapsulated) Decapsulate(arg multiaddr.Multiaddr) multiaddr.Multiaddr {
	return maddr.Multiaddr.Decapsulate(arg)
}

func (maddr *Encapsulated) ValueForProtocol(code int) (string, error) {
	return maddr.Multiaddr.ValueForProtocol(code)
}
