package fscmds

import (
	goerrors "errors"
	"fmt"
	"strings"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/manager"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const (
	mountParameter           = "mount"
	mountArgumentDescription = "Multiaddr style targets to bind to host. " + mountTargetExamples

	// shared
	mountStringArgument = "targets"
	mountTargetExamples = "(e.g. `/fuse/ipfs/path/ipfs /fuse/ipns/path/ipns ...`)"
)

var Mount = &cmds.Command{
	Arguments: []cmds.Argument{
		cmds.StringArg(mountStringArgument, false, true, mountArgumentDescription),
		// TODO: we should accept stdin + file arguments since we can
		// `ipfs mount mtab1.json mtab2.xml`,`... | ipfs mount -`
		// where everything just gets decoded into a flat list/stream:
		// (file|stdin data)[]byte -> (magic/header check + unmarshal) => []multiaddr
		//  ^post: for each file => combine maddrs
		// this would allow passing IPFS mtab references as well
		// e.g. `ipfs mount /ipfs/Qm.../my-mount-table.json`
	},
	Options: parameters.CmdsOptionsFrom((*settings)(nil)),
	PreRun:  mountPreRun,
	Run:     mountRun,
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatMount,
	},
	Encoders: cmds.Encoders,
	Helptext: cmds.HelpText{ // TODO: docs are still outdated - needs sys_ migrations
		Tagline:          MountTagline,
		ShortDescription: mountDescWhatAndWhere,
		LongDescription:  mountDescWhatAndWhere + "\nExample:\n" + mountDescExample,
	},
	Type:    manager.Response{},
	NoLocal: true, // always execute on fs service instance
}

// TODO: English pass; try to break apart code too, this is ~gross~ update: less gross, but still gross
// construct subcommand groups from supported API/ID pairs
// e.g. make these invocations equal
// 1) `ipfs mount /fuse/ipfs/path/mountpoint /fuse/ipfs/path/mountpoint2 ...
// 2) `ipfs mount fuse /ipfs/path/mountpoint /ipfs/path/mountpoint2 ...
// 3) `ipfs mount fuse ipfs /mountpoint /mountpoint2 ...
// allow things like `ipfs mount fuse -l` to list all fuse instances only, etc.
// shouldn't be too difficult to generate
// run re-executes `mount` with each arg prefixed `subreq.Args += api/id.String+arg`
func init() { registerMountSubcommands(Mount); registerMountSubcommands(Unmount); return }

// TODO: simplify and document
// prefix arguments with constants to make the CLI experience a little nicer to use
// TODO: filtered --list + helptext (use some fmt tmpl)
func registerMountSubcommands(parent *cmds.Command) {
	deriveArgs := func(args []cmds.Argument, subExamples string) []cmds.Argument {
		parentArgs := make([]cmds.Argument, 0, len(parent.Arguments))
		for _, arg := range parent.Arguments {
			if arg.Type == cmds.ArgString {
				arg.Name = "sub" + arg.Name
				arg.Required = true // NOTE: remove this if file support is added
				arg.Description = strings.ReplaceAll(arg.Description, mountTargetExamples, subExamples)
			}
			parentArgs = append(parentArgs, arg)
		}
		return parentArgs
	}

	template := &cmds.Command{
		Run:      parent.Run,
		PostRun:  parent.PostRun,
		Encoders: parent.Encoders,
		Type:     parent.Type,
		NoLocal:  parent.NoLocal,
		NoRemote: parent.NoRemote,
	}

	genPrerun := func(prefix string) func(request *cmds.Request, env cmds.Environment) error {
		return func(request *cmds.Request, env cmds.Environment) error {
			for i, arg := range request.Arguments {
				request.Arguments[i] = strings.TrimPrefix(prefix+arg, "//")
			}
			return parent.PreRun(request, env)
		}
	}

	subcommands := make(map[string]*cmds.Command)
	for _, api := range []filesystem.API{
		filesystem.Fuse,
		filesystem.Plan9Protocol,
	} {
		hostName := api.String()
		subsystems := make(map[string]*cmds.Command)

		com := new(cmds.Command)
		*com = *template
		prefix := fmt.Sprintf("/%s/", hostName)
		com.Arguments = deriveArgs(parent.Arguments, "(e.g. `/ipfs/path/ipfs /ipns/path/ipns ...`)")
		com.PreRun = genPrerun(prefix)
		com.Subcommands = subsystems
		subcommands[hostName] = com

		// TODO: we need 1 canonical "supported" array somewhere, defined at compile time; used by all
		// as-is there's a few of these literal arrays
		for _, id := range []filesystem.ID{
			filesystem.IPFS,
			filesystem.IPNS,
			filesystem.PinFS,
		} {
			nodeName := id.String()
			com := new(cmds.Command)
			*com = *template
			prefix := fmt.Sprintf("/%s/%s/path", hostName, nodeName)
			com.Arguments = deriveArgs(parent.Arguments, "(e.g. `/mnt/1 /mnt/2 ...`)")
			com.PreRun = genPrerun(prefix)
			subsystems[nodeName] = com
		}
	}
	parent.Subcommands = subcommands
}

func mountPreRun(request *cmds.Request, env cmds.Environment) (err error) {
	if len(request.Arguments) == 0 {
		return goerrors.New("no arguments provided - portable defaults not implemented yet")
		/* TODO: update defaults - don't depend on go-ipfs config file
		FIXME: if the argument is just a header,
		the dispatcher will send nil request(s) to that header's binder
		(if there is no IPFS config file to supply the request's target values)
		request.Arguments = []string{
			"/fuse/ipfs",
			"/fuse/ipns",
		}
		*/
	}
	return
}

func mountRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) (err error) {
	fsEnv, envIsUsable := env.(FileSystemEnvironment)
	if !envIsUsable {
		return envError(env)
	}

	fsi, err := fsEnv.Manager(request)
	if err != nil {
		return err
	}

	var (
		ctx                     = request.Context
		requests, requestErrors = manager.ParseArguments(ctx, request.Arguments...)
		responses               = fsi.Bind(ctx, requests)
		allErrs                 = emitResponses(ctx, emitter.Emit,
			requestErrors, responses)
	)

	return flattenErrors("mount", allErrs)
}

func formatMount(response cmds.Response, emitter cmds.ResponseEmitter) error {
	var (
		ctx                    = response.Request().Context
		stringArgs             = response.Request().Arguments
		outputs                = makeOptionalOutputs(response.Request(), emitter)
		responses, inputErrors = responseToResponses(ctx, response)

		msg = fmt.Sprintf("Attempting to bind to host system: %s...\n",
			strings.Join(stringArgs, ", "))
		err     = outputs.Print(msg)
		allErrs = renderToConsole(response.Request(), outputs,
			inputErrors, responses)
	)
	if err != nil {
		return err
	}
	return flattenErrors("mount", allErrs)
}
