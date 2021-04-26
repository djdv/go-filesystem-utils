package fscmds

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/adrg/xdg"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	// CmdsParameterSet defines an argument's description
	// and the set of keys to retrieve it from various sources.
	CmdsParameterSet struct {
		Name, Description, Environment string
	}

	// ServiceEnvironment provides an interface to the file system service.
	// Callers of service commands, should expected the routine to call `AssertEnvironment`
	// on the `env` provided to them, in order to gain access to this interface.
	ServiceEnvironment interface {
		context.Context
	}
)

var ErrServiceNotFound = errors.New("could not find service instance")

const (
	// ServiceName defines a default name which server and clients may use
	// to refer to the service, in namespace oriented APIs.
	// Effectively the service root.
	ServiceName = "fs"
	// ServerName defines a default name which servers and clients may use
	// to form or find connections to the named server instance.
	// (E.g. a Unix socket of path `.../$ServiceName/$ServerName`.)
	ServerName = "server"
)

// AssertEnvironment will check the environment provided,
// and return an interface to the local service controller.
func AssertEnvironment(env cmds.Environment) (ServiceEnvironment, error) {
	fsEnv, envIsUsable := env.(ServiceEnvironment)
	if !envIsUsable {
		return nil, cmds.Errorf(cmds.ErrClient,
			`environment type "%T" does not implement "%s"`,
			env,
			reflect.TypeOf((*ServiceEnvironment)(nil)).Elem().Name())
	}
	return fsEnv, nil
}

var (
	ServiceMaddrParameter = CmdsParameterSet{
		Name:        "api",
		Description: "File system service multiaddr to use.",
		Environment: "FS_API_MADDR",
	}
	AutoExitParameter = CmdsParameterSet{
		Name:        "stop-after",
		Description: `Time interval (e.g. "30s") to check if the service is active and exit if not.`,
		Environment: "FS_STOP_AFTER",
	}
)

func RootOptions() []cmds.Option {
	return []cmds.Option{
		cmds.OptionEncodingType,
		cmds.OptionTimeout,
		cmds.OptionStreamChannels,
		cmds.BoolOption(cmds.OptLongHelp, "Show the full command help text."),
		cmds.BoolOption(cmds.OptShortHelp, "Show a short version of the command help text."),
		cmds.StringOption(ServiceMaddrParameter.Name, ServiceMaddrParameter.Description),
		cmds.StringOption(AutoExitParameter.Name, AutoExitParameter.Description),
	}
}

func GetStringArgument(request *cmds.Request, parameters CmdsParameterSet) (argument string, provided bool, err error) {
	var cmdsArg interface{}
	if cmdsArg, provided = request.Options[parameters.Name]; provided {
		var isString bool
		if argument, isString = cmdsArg.(string); !isString {
			err = cmds.Errorf(cmds.ErrClient,
				"%s's argument %v is type: %T, expecting type: %T",
				parameters.Name, cmdsArg, cmdsArg, argument)
			return
		}
	} else {
		argument, provided = os.LookupEnv(parameters.Environment)
	}
	return
}

func GetBoolArgument(request *cmds.Request, parameters CmdsParameterSet) (argument, provided bool, err error) {
	var cmdsArg interface{}
	if cmdsArg, provided = request.Options[parameters.Name]; provided {
		var isBool bool
		if argument, isBool = cmdsArg.(bool); !isBool {
			err = cmds.Errorf(cmds.ErrClient,
				"%s's argument %v is type: %T, expecting type: %T",
				parameters.Name, cmdsArg, cmdsArg, provided)
			return
		}
	} else {
		var literalArgument string
		if literalArgument, provided = os.LookupEnv(parameters.Environment); provided {
			argument, err = strconv.ParseBool(literalArgument)
		}
	}
	return
}

func GetDurationArgument(request *cmds.Request, parameters CmdsParameterSet) (duration time.Duration, provided bool, err error) {
	var arg string
	if arg, provided, err = GetStringArgument(request, parameters); err != nil {
		return
	}
	if provided {
		duration, err = time.ParseDuration(arg)
	}
	return
}

func GetMultiaddrArgument(request *cmds.Request, parameters CmdsParameterSet) (maddr multiaddr.Multiaddr, provided bool, err error) {
	var arg string
	if arg, provided, err = GetStringArgument(request, parameters); err != nil {
		return
	}
	if provided {
		maddr, err = multiaddr.NewMultiaddr(arg)
	}
	return
}

// GetServiceMaddr will return the (resolved) multiaddr
// for the service multiaddr provided in the request.
// If a multiaddr is not provided, GetServiceMaddr
// will look for a local service instance and return its multiaddr.
// If no local instances are found,
// `ErrServiceNotFound` will be returned (wrapped).
func GetServiceMaddr(request *cmds.Request) (serviceMaddr multiaddr.Multiaddr, provided bool, err error) {
	serviceMaddr, provided, err = GetMultiaddrArgument(request, ServiceMaddrParameter)
	if err != nil {
		return
	}
	if !provided { // look in standard locations for active instance
		serviceMaddr, err = localServiceMaddr()
		return
	}
	if serviceMaddr, err = resolveAddr(request.Context, serviceMaddr); err != nil {
		err = fmt.Errorf("resolve service maddr \"%#v\": %w", serviceMaddr, err)
	}
	return
}

// localServiceMaddr returns a local service socket's maddr,
// if an active server instance is found.
func localServiceMaddr() (multiaddr.Multiaddr, error) {
	var (
		xdgName = filepath.Join(ServiceName, ServerName)
		xdgErr  error
	)
	for _, searchfunc := range []func(string) (string, error){
		xdg.SearchStateFile,
		xdg.SearchRuntimeFile,
		xdg.SearchConfigFile,
	} {
		servicePath, err := searchfunc(xdgName)
		if err != nil {
			if xdgErr == nil {
				xdgErr = err
			} else {
				xdgErr = fmt.Errorf("%s, %s", xdgErr, err)
			}
			continue
		}
		serviceMaddr, err := multiaddr.NewMultiaddr(path.Join("/unix/", filepath.ToSlash(servicePath)))
		if err != nil {
			return nil, err
		}
		if ClientDialable(serviceMaddr) {
			return serviceMaddr, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrServiceNotFound, xdgErr)
}

func resolveAddr(ctx context.Context, addr multiaddr.Multiaddr) (multiaddr.Multiaddr, error) {
	const resolveTimeout = 15 * time.Second // arbitrary
	ctx, cancelFunc := context.WithTimeout(ctx, resolveTimeout)
	defer cancelFunc()

	addrs, err := madns.DefaultResolver.Resolve(ctx, addr)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		return nil, errors.New("non-resolvable API endpoint")
	}

	return addrs[0], nil
}

// ClientDialable returns true if the multiaddr is dialable.
// Usually signifying the target service is ready for operation.
// Otherwise, it's down.
func ClientDialable(maddr multiaddr.Multiaddr) (connected bool) {
	socketPath, err := maddr.ValueForProtocol(multiaddr.P_UNIX)
	if err == nil {
		if runtime.GOOS == "windows" { // `/C:/path/...` -> `C:\path\...`
			socketPath = filepath.FromSlash(strings.TrimPrefix(socketPath, `/`))
		}
		fi, err := os.Lstat(socketPath)
		if err != nil {
			return false
		}

		// TODO: link issue tracker number
		// FIXME: [2021.04.30 / Go 1.16]
		// Go does not set socket mode on Windows
		// change this when resolved
		if runtime.GOOS != "windows" {
			return fi.Mode()&os.ModeSocket != 0
		}
		// HACK:
		// for now, try dialing the socket
		// but only when it exists, otherwise we'd create it
		// and need to clean that up
	}
	conn, err := manet.Dial(maddr)
	if err == nil && conn != nil {
		if err = conn.Close(); err != nil {
			return // socket is faulty, not accepting.
		}
		connected = true
	}
	return
}
