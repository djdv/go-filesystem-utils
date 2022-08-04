package commands

import (
	"errors"
	"flag"
	"fmt"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
)

type (
	commonSettings struct {
		command.HelpArg
		verbose bool
	}
	clientSettings struct {
		serverMaddr multiaddr.Multiaddr
		commonSettings
	}

	valueContainer[t any] struct {
		tPtr  *t
		parse func(string) (t, error)
	}
)

func (set *commonSettings) BindFlags(fs *flag.FlagSet) {
	set.HelpArg.BindFlags(fs)
	fs.BoolVar(&set.verbose, "verbose",
		false, "Enable log messages.")
}

func (set *clientSettings) BindFlags(fs *flag.FlagSet) {
	set.commonSettings.BindFlags(fs)
	multiaddrVar(fs, &set.serverMaddr, "maddr",
		multiaddr.StringCast(daemon.ServiceMaddr), "Server `maddr`.")
}

func containerVar[t any, parser func(string) (t, error)](fs *flag.FlagSet, tPtr *t,
	name string, defVal t, usage string,
	parse parser,
) {
	*tPtr = defVal
	fs.Var(valueContainer[t]{
		tPtr:  tPtr,
		parse: parse,
	}, name, usage)
}

func (vc valueContainer[t]) String() string {
	if vc.tPtr != nil {
		tVal := *vc.tPtr
		// Special cases.
		const invalidID = "nobody"
		switch id := any(tVal).(type) {
		case p9.UID:
			if !id.Ok() {
				return invalidID
			}
		case p9.GID:
			if !id.Ok() {
				return invalidID
			}
		}
		return fmt.Sprint(tVal)
	}
	return ""
}

func (vc valueContainer[t]) Set(arg string) error {
	tVal, err := vc.parse(arg)
	if err != nil {
		return err
	}
	if vc.tPtr == nil {
		vc.tPtr = new(t)
	}
	*vc.tPtr = tVal
	return nil
}

func multiaddrVar(fs *flag.FlagSet, maddrPtr *multiaddr.Multiaddr,
	name string, defVal multiaddr.Multiaddr, usage string,
) {
	containerVar(fs, maddrPtr, name, defVal, usage, multiaddr.NewMultiaddr)
}

func parseID[id p9.UID | p9.GID](arg string) (id, error) {
	num, err := strconv.ParseUint(arg, 0, 32)
	if err != nil {
		return 0, err
	}
	return id(num), nil
}

func uidVar(fs *flag.FlagSet, uidPtr *p9.UID,
	name string, defVal p9.UID, usage string,
) {
	containerVar(fs, uidPtr, name, defVal, usage, parseID[p9.UID])
}

func gidVar(fs *flag.FlagSet, gidPtr *p9.GID,
	name string, defVal p9.GID, usage string,
) {
	containerVar(fs, gidPtr, name, defVal, usage, parseID[p9.GID])
}

func closerKeyVar(fs *flag.FlagSet, keyPtr *[]byte,
	name string, defVal []byte, usage string,
) {
	containerVar(fs, keyPtr, name, defVal, usage, func(key string) ([]byte, error) {
		if *keyPtr != nil {
			return nil, errors.New("key provided multiple times")
		}
		return []byte(key), nil
	})
}
