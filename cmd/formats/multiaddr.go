package formats

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/multiformats/go-multiaddr"
)

// https://github.com/multiformats/go-multiaddr/issues/100
type Multiaddr struct{ Interface multiaddr.Multiaddr }

// TODO: move to test pkg
var _ multiaddr.Multiaddr = (*Multiaddr)(nil)

func (maddr Multiaddr) MarshalBinary() ([]byte, error) {
	return maddr.Interface.Bytes(), nil
}

func (maddr *Multiaddr) UnmarshalBinary(b []byte) error {
	decoded, err := multiaddr.NewMultiaddrBytes(b)
	if err != nil {
		return err
	}
	maddr.Interface = decoded
	return nil
}

func (maddr Multiaddr) MarshalText() ([]byte, error) {
	return []byte(maddr.Interface.String()), nil
}

func (maddr *Multiaddr) UnmarshalText(b []byte) error {
	decoded, err := multiaddr.NewMultiaddr(string(b))
	if err != nil {
		return err
	}
	maddr.Interface = decoded
	return nil
}

func (maddr Multiaddr) MarshalJSON() ([]byte, error) {
	if maddr.Interface == nil {
		return nil, fmt.Errorf("response's Request field must be populated")
	}
	return json.Marshal(maddr.Interface.Bytes())
}

func (maddr *Multiaddr) UnmarshalJSON(b []byte) error {
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

	maddr.Interface = maddrBytes
	return nil
}

func (maddr *Multiaddr) Equal(arg multiaddr.Multiaddr) bool { return maddr.Interface.Equal(arg) }
func (maddr *Multiaddr) Bytes() []byte                      { return maddr.Interface.Bytes() }
func (maddr *Multiaddr) String() string                     { return maddr.Interface.String() }
func (maddr *Multiaddr) Protocols() []multiaddr.Protocol    { return maddr.Interface.Protocols() }
func (maddr *Multiaddr) Encapsulate(arg multiaddr.Multiaddr) multiaddr.Multiaddr {
	return maddr.Interface.Encapsulate(arg)
}
func (maddr *Multiaddr) Decapsulate(arg multiaddr.Multiaddr) multiaddr.Multiaddr {
	return maddr.Interface.Decapsulate(arg)
}
func (maddr *Multiaddr) ValueForProtocol(code int) (string, error) {
	return maddr.Interface.ValueForProtocol(code)
}
