package executor_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	"github.com/djdv/go-filesystem-utils/cmd/executor"
	fscmds "github.com/djdv/go-filesystem-utils/cmd/fs/settings"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
	"github.com/multiformats/go-multiaddr"
)

func TestMain(m *testing.M) {
	// When called with service arguments,
	// call the service's main function.
	if len(os.Args) >= 2 &&
		os.Args[1] == service.Name &&
		os.Args[2] == daemon.Name {
		var (
			ctx  = context.Background()
			root = &cmds.Command{
				Options: fscmds.RootOptions(),
				Subcommands: map[string]*cmds.Command{
					service.Name: service.Command,
				},
			}
			err = cli.Run(ctx, root, os.Args,
				os.Stdin, os.Stdout, os.Stderr,
				environment.MakeEnvironment, executor.MakeExecutor)
		)
		if err != nil {
			var cliError cli.ExitError
			if errors.As(err, &cliError) {
				os.Exit(int(cliError))
			}
		}
		os.Exit(0)
	}
	// otherwise call Go's standard testing.Main
	os.Exit(m.Run())
}

func TestExecutor(t *testing.T) {
	t.Run("nil inputs", func(t *testing.T) {
		defer func() { recover() }() // nil args are expected to panic.
		_, err := executor.MakeExecutor(nil, nil)
		if err == nil {
			t.Fatal("executor was returned with nil constructor arguments")
		}
	})

	type testStruct struct {
		name    string
		cmdPath []string
	}
	const (
		rootCmdName   = "root"
		localCmdName  = "local"
		remoteCmdName = "remote"
	)
	var (
		remoteCmdTest = testStruct{
			name:    remoteCmdName,
			cmdPath: []string{remoteCmdName},
		}

		tests = []testStruct{
			{
				name:    rootCmdName,
				cmdPath: nil,
			},
			{
				name:    localCmdName,
				cmdPath: []string{localCmdName},
			},
			remoteCmdTest,
		}
		root = &cmds.Command{
			Subcommands: map[string]*cmds.Command{
				localCmdName: {NoRemote: true},
				remoteCmdName: {
					NoLocal: true,
					Run: func(*cmds.Request, cmds.ResponseEmitter, cmds.Environment) error {
						return nil
					},
				},
				service.Name: service.Command,
			},
		}
	)
	t.Run("No options", func(t *testing.T) {
		serviceMaddrs, err := daemon.UserServiceMaddrs()
		if err != nil {
			t.Fatal(err)
		}

		var (
			procLauncherIndex = len(tests) - 1
			subCmdEnv         = new(exec.Cmd)
		)
		for i, test := range tests {
			t.Run(test.name+" request", func(t *testing.T) {
				request, cmdsEnv := issueRequest(t, root, test.cmdPath, nil)
				var (
					execEnv    cmds.Environment
					useProcEnv = i == procLauncherIndex
				)
				if useProcEnv {
					execEnv = subCmdEnv
				} else {
					execEnv = cmdsEnv
				}

				if _, err := executor.MakeExecutor(request, execEnv); err != nil {
					t.Fatal(err)
				}

				if useProcEnv &&
					subCmdEnv.Process == nil {
					t.Fatal("subprocess was not returned by constructor/launcher")
				}
			})
		}

		t.Run("reuse instance", func(t *testing.T) {
			// FIXME: sometimes panics (go test -count=10 .\...)
			// because `.Process` is nil
			// ^ daemon is panicing and exiting abruptly?
			currentPid := subCmdEnv.Process.Pid
			request, _ := issueRequest(t, root, remoteCmdTest.cmdPath, nil)
			if _, err = executor.MakeExecutor(request, subCmdEnv); err != nil {
				t.Fatal(err)
			}
			if currentPid != subCmdEnv.Process.Pid {
				t.Fatal("subprocess was overwritten before exiting")
			}
		})

		for _, serviceMaddr := range serviceMaddrs {
			stopIntstance(t, serviceMaddr, root, subCmdEnv)
		}
	})

	t.Run("Stop special case", func(t *testing.T) {
		cmdPath := []string{service.Name, daemon.Name, stop.Name}
		request, serviceEnv := issueRequest(t, root, cmdPath, nil)
		if _, err := executor.MakeExecutor(request, serviceEnv); err != nil {
			if !errors.Is(err, executor.ErrCouldNotConnect) {
				t.Fatal(err)
			}
		}
	})

	t.Run("With options", func(t *testing.T) {
		var (
			serviceMaddr = multiaddr.StringCast("/ip4/127.0.0.1/tcp/0")
			optMap       = cmds.OptMap{
				// NOTE: We provide an address just to prevent the executor-constructor
				// from trying to spawn a server instance (which happens by default)
				fscmds.ServiceMaddrs().CommandLine(): []multiaddr.Multiaddr{
					serviceMaddr,
				},
			}
		)
		for _, test := range tests {
			t.Run(test.name+" request", func(t *testing.T) {
				request, serviceEnv := issueRequest(t, root, test.cmdPath, optMap)
				if _, err := executor.MakeExecutor(request, serviceEnv); err != nil {
					if !errors.Is(err, executor.ErrCouldNotConnect) {
						t.Fatal(err)
					}
				}
			})
		}
	})
}

func stopIntstance(t *testing.T,
	serviceMaddr multiaddr.Multiaddr, root *cmds.Command,
	subCmd *exec.Cmd) {
	client, err := daemon.GetClient(serviceMaddr)
	if err != nil {
		t.Fatal(err)
	}
	stopRequest, err := cmds.NewRequest(context.Background(),
		append(daemon.CmdsPath(), stop.Name),
		nil, nil, nil, root,
	)
	if err != nil {
		t.Fatal(err)
	}

	var (
		emitter, response = cmds.NewChanResponsePair(stopRequest)
		responseErrs      = make(chan error, 1)
		execErrs          = make(chan error, 1)
	)

	go func() {
		defer close(responseErrs)
		for {
			_, err := response.Next()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					responseErrs <- err
				}
				return
			}
		}
	}()

	go func() {
		defer close(execErrs)
		if err := client.Execute(stopRequest, emitter, nil); err != nil {
			execErrs <- err
		}
	}()

	for execErrs != nil ||
		responseErrs != nil {
		var (
			err error
			ok  bool
		)
		select {
		case err, ok = <-execErrs:
			if !ok {
				execErrs = nil
				continue
			}
		case err, ok = <-responseErrs:
			if !ok {
				responseErrs = nil
				continue
			}
		}
		t.Fatal(err)
	}

	if err := subCmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func issueRequest(t *testing.T, root *cmds.Command,
	path []string, optMap cmds.OptMap) (*cmds.Request, cmds.Environment) {
	ctx := context.Background()
	request, err := cmds.NewRequest(ctx, path, optMap,
		nil, nil, root)
	if err != nil {
		t.Fatal(err)
	}

	serviceEnv, err := environment.MakeEnvironment(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	return request, serviceEnv
}
