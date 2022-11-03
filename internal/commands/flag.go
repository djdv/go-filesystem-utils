package commands

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
	giconfig "github.com/ipfs/kubo/config"
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
	lazyFlag[T any]    interface{ get() (T, error) }
	defaultServerMaddr struct{ multiaddr.Multiaddr }
	defaultIPFSMaddr   struct{ multiaddr.Multiaddr }
)

func (defaultServerMaddr) get() (multiaddr.Multiaddr, error) {
	userMaddrs, err := daemon.UserServiceMaddrs()
	if err != nil {
		return nil, err
	}
	return userMaddrs[0], nil
}

func (ds defaultServerMaddr) String() string {
	maddr, err := ds.get()
	if err != nil {
		return ""
	}
	return maddr.String()
}

func (defaultIPFSMaddr) get() (multiaddr.Multiaddr, error) {
	maddrs, err := getIPFSAPI()
	if err != nil {
		return nil, err
	}
	return maddrs[0], nil
}

func (di defaultIPFSMaddr) String() string {
	maddr, err := di.get()
	if err != nil {
		return "no IPFS API file found (must provide this argument)"
	}
	return maddr.String()
}

func (set *commonSettings) BindFlags(fs *flag.FlagSet) {
	set.HelpArg.BindFlags(fs)
	fs.BoolVar(&set.verbose, "verbose",
		false, "Enable log messages.")
}

func (set *clientSettings) BindFlags(fs *flag.FlagSet) {
	set.commonSettings.BindFlags(fs)
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
	// XXX: Special cases.
	// TODO Handle this better. Optional helptext string in the constructor?
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

func getIPFSAPI() ([]multiaddr.Multiaddr, error) {
	location, err := getIPFSAPIPath()
	if err != nil {
		return nil, err
	}
	if !apiFileExists(location) {
		return nil, errors.New("IPFS API file not found") // TODO: proper error value
	}
	return parseIPFSAPI(location)
}

func getIPFSAPIPath() (string, error) {
	const apiFile = "api"
	var target string
	if ipfsPath, set := os.LookupEnv(giconfig.EnvDir); set {
		target = filepath.Join(ipfsPath, apiFile)
	} else {
		target = filepath.Join(giconfig.DefaultPathRoot, apiFile)
	}
	return expandHomeShorthand(target)
}

func expandHomeShorthand(name string) (string, error) {
	if !strings.HasPrefix(name, "~") {
		return name, nil
	}
	homeName, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeName, name[1:]), nil
}

func apiFileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func parseIPFSAPI(name string) ([]multiaddr.Multiaddr, error) {
	// NOTE: [upstream problem]
	// If the config file has multiple API maddrs defined,
	// only the first one will be contained in the API file.
	maddrString, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	maddr, err := multiaddr.NewMultiaddr(string(maddrString))
	if err != nil {
		return nil, err
	}
	return []multiaddr.Multiaddr{maddr}, nil
}

/* TODO: [lint] we might still want to use this method instead. Needs consideration.
func ipfsAPIFromConfig([]multiaddr.Multiaddr, error) {
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
*/
