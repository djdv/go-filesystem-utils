package commands

import (
	"flag"
	"fmt"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
)

type (
	commonSettings struct {
		command.HelpArg
		verbose bool
	}
	clientSettings struct {
		serviceMaddr multiaddr.Multiaddr
		commonSettings
	}

	valueContainer[t any] struct {
		tPtr  *t
		parse func(string) (t, error)
	}

	// TODO: [31f421d5-cb4c-464e-9d0f-41963d0956d1]
	// We should formalize this into the command package.
	// So lazy flags can be defined easily, and initialized
	// as late as possible. (Right before calling execute.)
	// Exec function itself shouldn't need to do this.
	lazyFlag[T any]    interface{ get() T }
	defaultServerMaddr struct{ multiaddr.Multiaddr }
)

func (defaultServerMaddr) get() multiaddr.Multiaddr {
	userMaddrs, err := daemon.UserServiceMaddrs()
	if err != nil {
		panic(err)
	}
	return userMaddrs[0]
}

func (set *commonSettings) BindFlags(fs *flag.FlagSet) {
	set.HelpArg.BindFlags(fs)
	fs.BoolVar(&set.verbose, "verbose",
		false, "Enable log messages.")
}

func (set *clientSettings) BindFlags(fs *flag.FlagSet) {
	set.commonSettings.BindFlags(fs)
	// TODO: Can we format these nicely?
	// Multiple UDS paths are long when right justified in `-help`.
	// One way could be to truncate duplicate string prefixes:
	// `/unix/C:\Users\...`; \dirA\1.sock dirB\2.sock`
	// Another as a tree:
	// /unix/
	//   /fullPath1
	//   /fullPath2
	multiaddrVar(fs, &set.serviceMaddr, daemon.ServerName,
		defaultServerMaddr{}, "File system service `maddr`.")
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

func (vc valueContainer[T]) String() string {
	tPtr := vc.tPtr
	if tPtr == nil {
		return ""
	}

	tVal := *tPtr
	if lazy, ok := any(tVal).(lazyFlag[T]); ok {
		tVal = lazy.get() // TODO: [31f421d5-cb4c-464e-9d0f-41963d0956d1]
	}

	// XXX: Special cases.
	// TODO Handle this better. Optional `defaultString` in the constructor?
	const invalidID = "nobody"
	switch valType := any(tVal).(type) {
	case p9.UID:
		if !valType.Ok() {
			return invalidID
		}
	case p9.GID:
		if !valType.Ok() {
			return invalidID
		}
	}
	// Regular.
	return fmt.Sprint(tVal)
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
	const idSize = 32
	num, err := strconv.ParseUint(arg, 0, idSize)
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

func fsIDVar(fs *flag.FlagSet, fsidPtr *filesystem.ID,
	name string, defVal filesystem.ID, usage string,
) {
	containerVar(fs, fsidPtr, name, defVal, usage, filesystem.ParseID)
}

func fsAPIVar(fs *flag.FlagSet, fsAPIPtr *filesystem.API,
	name string, defVal filesystem.API, usage string,
) {
	containerVar(fs, fsAPIPtr, name, defVal, usage, filesystem.ParseAPI)
}
