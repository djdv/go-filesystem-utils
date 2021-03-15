package fscmds

import (
	"os"
	"strconv"
	"time"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

// CmdsParameterSet defines an argument's description
// and the set of keys to retrieve it from various sources.
type CmdsParameterSet struct {
	Name, Description, Environment string
}

const (
	// ServiceName defines a default name servers and clients may use
	// to refer to the service via namespace oriented APIs.
	// Effectively the service root.
	ServiceName = "fs"
	// ServerName defines a default name servers and clients may use
	// to form or find connections to the named server instance.
	// (E.g. a Unix socket of path `.../$ServiceName/$ServerName`.)
	ServerName = "server"
)

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

	Options = []cmds.Option{
		// TODO: consider file-relevant cmds pkg options (symlinks, hidden attribute, etc.)
		// for dealing with fs/mtab-like input file
		cmds.OptionEncodingType,
		cmds.OptionTimeout,
		cmds.OptionStreamChannels,
		cmds.BoolOption(cmds.OptLongHelp, "Show the full command help text."),
		cmds.BoolOption(cmds.OptShortHelp, "Show a short version of the command help text."),
		cmds.StringOption(ServiceMaddrParameter.Name, ServiceMaddrParameter.Description),
		cmds.StringOption(AutoExitParameter.Name, AutoExitParameter.Description),
	}
)

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
