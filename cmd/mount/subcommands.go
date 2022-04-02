package mount

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type subcmdsMap map[string]*cmds.Command

// CLI shorthand generator
// `mount apiName fsName arg1 arg2 arg3...`
//             versus
// `mount --system=apiName --fs=fsName arg1 arg2 arg3...`
func registerMountSubcommands(parent *cmds.Command) {
	var (
		template = &cmds.Command{
			Arguments: []cmds.Argument{
				cmds.StringArg(ArgumentName, false, true,
					ArgumentDescription+" "+descriptionString(false, examplePaths()),
				),
			},
			PreRun:   parent.PreRun,
			Run:      parent.Run,
			PostRun:  parent.PostRun,
			Encoders: parent.Encoders,
			Type:     parent.Type,
			NoLocal:  parent.NoLocal,
			NoRemote: parent.NoRemote,
		}
		hosts = []filesystem.API{
			filesystem.Fuse,
			// filesystem.Plan9Protocol,
		}
		fsids = []filesystem.ID{
			filesystem.IPFS,
			filesystem.IPNS,
			filesystem.PinFS,
			filesystem.KeyFS,
		}
	)
	// TODO: review + export (share with Unmount)
	parent.Subcommands = registerHostAPICmds(template, hosts, fsids)
}

func registerHostAPICmds(template *cmds.Command,
	hosts []filesystem.API, fsIDs []filesystem.ID,
) subcmdsMap {
	var (
		subcommands  = make(subcmdsMap, len(hosts))
		hostAPIParam = settings.HostAPIParam
		apiCommand   = new(cmds.Command)
	)
	*apiCommand = *template
	apiCommand.PreRun = func(request *cmds.Request, env cmds.Environment) error {
		if tpr := template.PreRun; tpr != nil {
			if err := tpr(request, env); err != nil {
				return err
			}
		}

		cmdPath := request.Path
		if err := checkSubCmd(3, cmdPath); err != nil {
			return err
		}

		subCmdName := cmdPath[len(cmdPath)-2]
		request.SetOption(hostAPIParam, strings.ToLower(subCmdName))
		return nil
	}
	for _, api := range hosts {
		hostAPIName := strings.ToLower(api.String())
		apiCommand.Subcommands = registerSystemIDCmds(apiCommand, fsIDs)
		subcommands[hostAPIName] = apiCommand
	}
	return subcommands
}

func registerSystemIDCmds(template *cmds.Command, fsIDs []filesystem.ID) subcmdsMap {
	var (
		subsystems  = make(subcmdsMap, len(fsIDs))
		fsIDParam   = settings.FileSystemIDParam
		fsIDCommand = new(cmds.Command)
	)
	*fsIDCommand = *template
	fsIDCommand.PreRun = func(request *cmds.Request, env cmds.Environment) error {
		if pr := template.PreRun; pr != nil {
			if err := pr(request, env); err != nil {
				return err
			}
		}

		if len(request.Arguments) == 0 {
			return cmds.ClientError("This command requires arguments.")
		}

		var (
			cmdPath    = request.Path
			subCmdName = cmdPath[len(cmdPath)-1]
		)
		request.SetOption(fsIDParam, strings.ToLower(subCmdName))

		for i, arg := range request.Arguments {
			// HACK: we need a better interface here for platform specifics
			if runtime.GOOS == "windows" {
				if len(arg) == 2 &&
					unicode.IsLetter(rune(arg[0])) &&
					arg[1] == ':' {
					request.Arguments[i] = "/path/" + arg
					continue
				}
			}
			mountPoint, err := filepath.Abs(maybeExpandArg(arg))
			if err != nil {
				return err
			}
			request.Arguments[i] = "/path/" + mountPoint
		}

		return nil
	}

	for _, fsID := range fsIDs {
		goFSName := strings.ToLower(fsID.String())
		subsystems[goFSName] = fsIDCommand
	}
	return subsystems
}

func checkSubCmd(depth int, path []string) error {
	if cmdIsSubcmd := len(path) >= depth; !cmdIsSubcmd {
		return cmds.ClientError("This command needs to called with a subcommand.")
	}
	return nil
}

func maybeExpandArg(path string) string {
	return os.ExpandEnv(
		maybeExpandTilde(path),
	)
}

func maybeExpandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	return filepath.Join(usr.HomeDir, (path)[1:])
}
