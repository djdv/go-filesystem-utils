package mountpoint

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

type (
	// Mounter is capable of binding a guest file system
	// to its host system.
	Mounter interface {
		Mount(fsys fs.FS) (io.Closer, error)
	}
	// FSMaker is capable of instantiating a guest file system.
	FSMaker interface {
		MakeFS() (fs.FS, error)
	}
	// Marshaler enforces the encoding scheme used by this package.
	Marshaler interface {
		json.Marshaler
		json.Unmarshaler
	}
	// Host is any type capable of encoding itself
	// and mounting a guest file system.
	Host interface {
		Mounter
		Marshaler
	}
	// Guest is any type capable of encoding itself
	// and instantiating a guest file system.
	Guest interface {
		FSMaker
		Marshaler
	}
	// Pair joins both portions of a mountpoint,
	// along with identifiers describing them.
	Pair struct {
		host    Host
		guest   Guest
		hostID  filesystem.Host
		guestID filesystem.ID
	}
	tag struct {
		filesystem.Host `json:"host"`
		filesystem.ID   `json:"guest"`
	}
	pairMarshaler struct {
		Tag   tag             `json:"tag"`
		Host  json.RawMessage `json:"host"`
		Guest json.RawMessage `json:"guest"`
	}
	// FieldParser should parse and assign its arguments.
	// Returning either a [FieldError] if the attribute
	// is not applicable, or any other error if the value is invalid.
	FieldParser interface {
		ParseField(attribute, value string) error
	}
	// FieldError describes which attribute was searched for
	// and those available which were tried.
	// Useful for chaining [FieldParser].ParseField calls with [errors.As].
	FieldError struct {
		Attribute string
		Tried     []string
	}
)

func NewPair(
	hostID filesystem.Host, guestID filesystem.ID,
	host Host, guest Guest,
) *Pair {
	return &Pair{
		hostID:  hostID,
		guestID: guestID,
		host:    host,
		guest:   guest,
	}
}

func SplitData(pairData []byte) (_ tag, host, guest json.RawMessage, _ error) {
	var pair pairMarshaler
	if err := json.Unmarshal(pairData, &pair); err != nil {
		return tag{}, nil, nil, err
	}
	return pair.Tag, pair.Host, pair.Guest, nil
}

func (pr *Pair) MarshalJSON() ([]byte, error) {
	hostData, err := json.Marshal(pr.host)
	if err != nil {
		return nil, err
	}
	guestData, err := json.Marshal(pr.guest)
	if err != nil {
		return nil, err
	}
	return json.Marshal(pairMarshaler{
		Tag: tag{
			Host: pr.hostID,
			ID:   pr.guestID,
		},
		Host:  hostData,
		Guest: guestData,
	})
}

func (pr *Pair) ParseField(key, value string) error {
	const (
		hostPrefix     = "host."
		guestPrefix    = "guest."
		unsupportedFmt = "%w: %T does not implement field parser"
	)
	var (
		prefix string
		parser FieldParser
	)
	switch {
	case strings.HasPrefix(key, hostPrefix):
		prefix = hostPrefix
		var ok bool
		if parser, ok = pr.host.(FieldParser); !ok {
			return fmt.Errorf(
				unsupportedFmt,
				errors.ErrUnsupported, pr.host,
			)
		}
	case strings.HasPrefix(key, guestPrefix):
		prefix = guestPrefix
		var ok bool
		if parser, ok = pr.guest.(FieldParser); !ok {
			return fmt.Errorf(
				unsupportedFmt,
				errors.ErrUnsupported, pr.guest,
			)
		}
	default:
		const wildcard = "*"
		return FieldError{
			Attribute: key,
			Tried:     []string{hostPrefix + wildcard, guestPrefix + wildcard},
		}
	}
	var (
		baseKey = key[len(prefix):]
		err     = parser.ParseField(baseKey, value)
	)
	if err == nil {
		return nil
	}
	var fErr FieldError
	if !errors.As(err, &fErr) {
		return err
	}
	tried := fErr.Tried
	for i, e := range fErr.Tried {
		tried[i] = prefix + e
	}
	fErr.Tried = tried
	return fErr
}

func (pr *Pair) UnmarshalJSON(data []byte) error {
	var pair pairMarshaler
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if err := pr.host.UnmarshalJSON(pair.Host); err != nil {
		return err
	}
	if err := pr.guest.UnmarshalJSON(pair.Guest); err != nil {
		return err
	}
	return nil
}

func (pr *Pair) MakeFS() (fs.FS, error) {
	return pr.guest.MakeFS()
}

func (pr *Pair) Mount(fsys fs.FS) (io.Closer, error) {
	return pr.host.Mount(fsys)
}

func (fe FieldError) Error() string {
	// Format:
	// unexpected key: "${key}", want one of: $QuotedCSV(${tried})
	const (
		delimiter  = ','
		space      = ' '
		separator  = string(delimiter) + string(space)
		separated  = len(separator)
		surrounder = '"'
		surrounded = len(string(surrounder)) * 2
		padding    = surrounded + separated
		gotPrefix  = "unexpected key: "
		wantPrefix = "want one of: "
		prefixes   = len(gotPrefix) + surrounded +
			len(wantPrefix) + separated
	)
	var (
		b    strings.Builder
		key  = fe.Attribute
		size = prefixes + len(key)
	)
	for i, tried := range fe.Tried {
		size += len(tried) + surrounded
		if i != 0 {
			size += separated
		}
	}
	b.Grow(size)
	b.WriteString(gotPrefix)
	b.WriteRune(surrounder)
	b.WriteString(key)
	b.WriteRune(surrounder)
	b.WriteString(separator)
	b.WriteString(wantPrefix)
	end := len(fe.Tried) - 1
	for i, tried := range fe.Tried {
		b.WriteRune(surrounder)
		b.WriteString(tried)
		b.WriteRune(surrounder)
		if i != end {
			b.WriteString(separator)
		}
	}
	return b.String()
}
