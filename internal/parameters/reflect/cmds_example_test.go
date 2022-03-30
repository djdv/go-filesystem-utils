package reflect_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
)

type (
	RootCommandSettings struct {
		Whatever  bool `parameters:"settings"`
		Whatever2 bool
	}

	ChildCommandSettings struct {
		RootCommandSettings
		SubWhatever  bool
		SubWhatever2 bool
	}
)

func ExampleMustMakeCmdsOptions() {
	var (
		subcmdName = "subcmd"
		subcmd     = &cmds.Command{
			Options: parameters.MustMakeCmdsOptions((*ChildCommandSettings)(nil)),
			Helptext: cmds.HelpText{
				Tagline: "does subcommand things",
			},
			Run: func(r *cmds.Request, re cmds.ResponseEmitter, e cmds.Environment) error {
				var (
					settings = new(ChildCommandSettings)
					sources  = []parameters.SetFunc{
						parameters.SettingsFromCmds(r),
					}
				)
				if err := parameters.Parse(r.Context, settings, sources); err != nil {
					return err
				}

				// ...

				return nil
			},
		}
		rootCommand = &cmds.Command{
			Options: parameters.MustMakeCmdsOptions(
				(*RootCommandSettings)(nil),
				parameters.WithBuiltin(true),
			),
			Helptext: cmds.HelpText{
				Tagline: "does root command things",
			},
			Run: func(r *cmds.Request, re cmds.ResponseEmitter, e cmds.Environment) error {
				var (
					settings = new(RootCommandSettings)
					sources  = []parameters.SetFunc{
						parameters.SettingsFromCmds(r),
					}
				)
				if err := parameters.Parse(r.Context, settings, sources); err != nil {
					return err
				}

				// ...

				return nil
			},
			Subcommands: map[string]*cmds.Command{
				subcmdName: subcmd,
			},
		}
		exampleEnv = func(context.Context, *cmds.Request) (cmds.Environment, error) {
			return nil, nil
		}
		exampleExec = func(req *cmds.Request, _ interface{}) (cmds.Executor, error) {
			return cmds.NewExecutor(req.Root), nil
		}

		ourName = filepath.Base(os.Args[0])
		cmdline = []string{
			strings.TrimSuffix(ourName, filepath.Ext(ourName)),
			"-" + cmds.OptShortHelp,
		}
	)

	cli.Run(context.TODO(), rootCommand, cmdline,
		os.Stdin, os.Stdout, os.Stderr,
		exampleEnv, exampleExec,
	)

	cmdline = []string{
		strings.TrimSuffix(ourName, filepath.Ext(ourName)),
		subcmdName,
		"-" + cmds.OptShortHelp,
	}
	cli.Run(context.TODO(), rootCommand, cmdline,
		os.Stdin, os.Stdout, os.Stderr,
		exampleEnv, exampleExec,
	)

	// Output:
	// USAGE
	//   parameters.test  - does root command things
	//
	//   parameters.test [--opt-1] [--opt-2] [--encoding=<encoding> | --enc]
	//                   [--timeout=<timeout>] [--stream-channels] [--help] [-h]
	//
	// SUBCOMMANDS
	//   parameters.test subcmd - does subcommand things
	//
	// USAGE
	//   parameters.test subcmd - does subcommand things
	//
	//   parameters.test subcmd [--opt-3] [--opt-4]
	//
	//   For more information about each command, use:
	//   'parameters.test subcmd <subcmd> --help'
}

func (*RootCommandSettings) Parameters() parameters.Parameters {
	return parameters.Parameters{
		parameters.NewParameter(
			"",
			parameters.WithRootNamespace(),
			parameters.WithName("opt1"),
		),
		parameters.NewParameter(
			"",
			parameters.WithRootNamespace(),
			parameters.WithName("opt2"),
		),
	}
}

func (*ChildCommandSettings) Parameters() parameters.Parameters {
	return append(
		(*RootCommandSettings)(nil).Parameters(),
		[]parameters.Parameter{
			parameters.NewParameter(
				"",
				parameters.WithName("opt3"),
			),
			parameters.NewParameter(
				"",
				parameters.WithName("opt4"),
			),
		}...)
}
