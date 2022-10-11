package commands

import (
	"flag"
	"fmt"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
	giconfig "github.com/ipfs/kubo/config"
	giconfigfile "github.com/ipfs/kubo/config/serialize"
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
	defaultIPFSMaddr   struct{ multiaddr.Multiaddr }
)

func (defaultServerMaddr) get() multiaddr.Multiaddr {
	userMaddrs, err := daemon.UserServiceMaddrs()
	if err != nil {
		panic(err)
	}
	return userMaddrs[0]
}

func (defaultIPFSMaddr) get() multiaddr.Multiaddr {
	// FIXME: we need to make it clear in the helptext that this is a dynamic value.
	// This will probably require changes to the command library.
	// E.g don't print `C:\some-sock` at the time of request,
	// print the value sources `$IPFS_API, ~/.ipfs/config, ...`.
	// ^ These are:
	/*
		const apiFile = "api"
		envAPI  = filepath.Join(giconfig.EnvDir, apiFile)
		fileAPI = filepath.Join(giconfig.DefaultPathRoot, apiFile)
	*/
	maddrs, err := ipfsAPIFromSystem()
	if err != nil {
		// FIXME: we need some way to fail gracefully.
		// Separate value from helptext and return some text saying we can't retrieve it.
		// Error out in the actual execute function.
		panic(err)
	}
	return maddrs[0]
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


func ipfsAPIFromSystem() ([]multiaddr.Multiaddr, error) {
	// TODO: We don't need, nor want the full config.
	// - IPFS doesn't declare a standard environment variable to use for the API.
	// We should declare and document our own to avoid touching the fs at all.
	// - The API file format is unlikely to change, we should probably just parse it by hand.
	// (The full config file contains node secrets
	// and I really don't want to pull those into memory at all.)
	// ^ We should try to coordinate upstream. Something this common should really be standardized.
	confFile, err := giconfig.Filename("", "")
	if err != nil {
		return nil, err
	}
	nodeConf, err := giconfigfile.Load(confFile)
	if err != nil {
		return nil, err
	}
	var (
		apiMaddrStrings = nodeConf.Addresses.API
		apiMaddrs       = make([]multiaddr.Multiaddr, len(apiMaddrStrings))
	)
	for i, maddrString := range apiMaddrStrings {
		maddr, err := multiaddr.NewMultiaddr(maddrString)
		if err != nil {
			return nil, err
		}
		apiMaddrs[i] = maddr
	}
	return apiMaddrs, nil
}
