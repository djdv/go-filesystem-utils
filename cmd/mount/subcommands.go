package mount

import (
	"strings"

	fscmds "github.com/djdv/go-filesystem-utils/filesystem/cmds"
	"github.com/djdv/go-filesystem-utils/filesystem"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func init() { registerMountSubcommands(Command) }

// CLI shorthand generator
// `mount apiName fsName arg1 arg2 arg3...`
//             versus
// `mount --system=apiName --fs=fsName arg1 arg2 arg3...`
func registerMountSubcommands(parent *cmds.Command) {
	// TODO: hoist level 2 up to level 1 as well
	//`mount ipfs args...` should be valid too and use the default API for the platform
	// TODO: review + export (share with Unmount)

	// Level 0 - copy of `mount`
	template := &cmds.Command{
		Arguments: parent.Arguments,
		Run:       parent.Run,
		PostRun:   parent.PostRun,
		Encoders:  parent.Encoders,
		Type:      parent.Type,
		NoLocal:   parent.NoLocal,
		NoRemote:  parent.NoRemote,
	}

	// Level 1 - Host system APIs - `mount 9p args...`
	var (
		subcommands   = make(map[string]*cmds.Command)
		hostParameter = fscmds.SystemAPI().CommandLine()
	)
	for _, api := range []filesystem.API{
		filesystem.Fuse,
		filesystem.Plan9Protocol,
	} {
		var (
			hostAPIName = strings.ToLower(api.String())
			subsystems  = make(map[string]*cmds.Command)
			apiCommand  = new(cmds.Command)
			apiPreRun   = func(request *cmds.Request, env cmds.Environment) error {
				if parent.PreRun != nil {
					return parent.PreRun(request, env)
				}
				request.SetOption(hostParameter, hostAPIName)
				return nil
			}
		)
		*apiCommand = *template
		apiCommand.PreRun = apiPreRun
		apiCommand.Subcommands = subsystems
		subcommands[hostAPIName] = apiCommand

		// Level 2 - Go file system APIs - `mount 9p ipfs args...`
		idParameter := fscmds.SystemID().CommandLine()
		for _, id := range []filesystem.ID{
			filesystem.IPFS,
			filesystem.IPNS,
			filesystem.PinFS,
			filesystem.KeyFS,
		} {
			var (
				goFSName  = strings.ToLower(id.String())
				idCommand = new(cmds.Command)
			)
			*idCommand = *template
			idCommand.PreRun = func(request *cmds.Request, env cmds.Environment) error {
				if err := apiPreRun(request, env); err != nil {
					return err
				}
				request.SetOption(idParameter, goFSName)
				return nil
			}
			subsystems[goFSName] = idCommand
		}
	}
	parent.Subcommands = subcommands
}
