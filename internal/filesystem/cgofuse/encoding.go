package cgofuse

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	"github.com/djdv/go-filesystem-utils/internal/generic"
)

type (
	// Mounter represents a set of marshalable values
	// that can be used to mount an [FS] instance.
	// Suitable for RPC, storage, etc.
	Mounter struct {
		Point           string   `json:"point"`
		LogPrefix       string   `json:"logPrefix"`
		Options         []string `json:"options"`
		DenyDeletePaths []string `json:"denyDeletePaths"`
		UID             *uint32  `json:"uid"`
		GID             *uint32  `json:"gid"`
		ReaddirPlus     *bool    `json:"readdirPlus"`
		CaseInsensitive *bool    `json:"caseInsensitive"`
		sysquirks                // Platform+runtime specific behavior.
	}
)

// Valid attribute names of [Mounter.ParseField].
const (
	PointAttribute           = "point"
	LogPrefixAttribute       = "logPrefix"
	OptionsAttribute         = "options"
	DenyDeleteAttribute      = "denyDeletePaths"
	UIDAttribute             = "uid"
	GIDAttribute             = "gid"
	ReaddirPlusAttribute     = "readdirPlus"
	CaseInsensitiveAttribute = "caseInsensitive"
)

func (settings *Mounter) Mount(fsys fs.FS) (io.Closer, error) {
	settings.sysquirks.mountHook()
	const (
		required = 0
		maximum  = required + 7
	)
	options := make([]Option, required, maximum)
	if prefix := settings.LogPrefix; prefix != "" {
		logger := log.New(os.Stdout, prefix, log.Lshortfile)
		options = append(options, WithLog(logger))
	}
	if len(settings.Options) > 0 {
		options = append(options, WithRawOptions(settings.Options...))
	}
	if len(settings.DenyDeletePaths) > 0 {
		options = append(options, DenyDelete(settings.DenyDeletePaths...))
	}
	if uid := settings.UID; uid != nil {
		options = append(options, WithUID(*uid))
	}
	if gid := settings.GID; gid != nil {
		options = append(options, WithGID(*gid))
	}
	if rdPlus := settings.ReaddirPlus; rdPlus != nil {
		options = append(options, CanReaddirPlus(*rdPlus))
	}
	if caseIns := settings.CaseInsensitive; caseIns != nil {
		options = append(options, IsCaseInsensitive(*caseIns))
	}
	closer, err := Mount(settings.Point, fsys, options...)
	if err != nil {
		return nil, err
	}
	return generic.Closer(func() error {
		settings.sysquirks.unmountHook()
		return closer.Close()
	}), nil
}

func (settings *Mounter) MarshalJSON() ([]byte, error) {
	type shadow Mounter
	return json.Marshal((*shadow)(settings))
}

func (settings *Mounter) UnmarshalJSON(data []byte) error {
	type shadow Mounter
	return json.Unmarshal(data, (*shadow)(settings))
}

func (settings *Mounter) ParseField(attribute, value string) error {
	var err error
	switch attribute {
	case PointAttribute:
		settings.Point = value
	case LogPrefixAttribute:
		settings.LogPrefix = value
	case OptionsAttribute:
		settings.Options = splitArgv(value)
	case DenyDeleteAttribute:
		csvR := csv.NewReader(strings.NewReader(value))
		csvR.TrimLeadingSpace = true
		var paths []string
		if paths, err = csvR.Read(); err == nil {
			settings.DenyDeletePaths = paths
		}
	case UIDAttribute:
		var uid uint32
		if uid, err = parseID(value); err == nil {
			settings.UID = &uid
		}
	case GIDAttribute:
		var gid uint32
		if gid, err = parseID(value); err == nil {
			settings.GID = &gid
		}
	case ReaddirPlusAttribute:
		var canReadDir bool
		if canReadDir, err = strconv.ParseBool(value); err == nil {
			settings.ReaddirPlus = &canReadDir
		}
	case CaseInsensitiveAttribute:
		var caseInsensative bool
		if caseInsensative, err = strconv.ParseBool(value); err == nil {
			settings.CaseInsensitive = &caseInsensative
		}
	default:
		err = mountpoint.FieldError{
			Attribute: attribute,
			Tried: []string{
				PointAttribute, LogPrefixAttribute,
				OptionsAttribute, DenyDeleteAttribute,
				UIDAttribute, GIDAttribute,
				ReaddirPlusAttribute, CaseInsensitiveAttribute,
			},
		}
	}
	return err
}

func parseID(value string) (uint32, error) {
	actual, err := strconv.ParseUint(value, 0, 32)
	if err != nil {
		return 0, err
	}
	return uint32(actual), nil
}

func splitArgv(argv string) (options []string) {
	var (
		tokens   = strings.Split(argv, "-")
		isDouble bool
	)
	for _, token := range tokens[1:] {
		if token == "" {
			isDouble = true
			continue
		}
		var option string
		token = strings.TrimSuffix(token, " ")
		if isDouble {
			option = "--" + token
			isDouble = false
		} else {
			option = "-" + token
		}
		options = append(options, option)
	}
	return options
}
