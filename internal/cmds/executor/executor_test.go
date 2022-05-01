package executor_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/service"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmdslib "github.com/djdv/go-filesystem-utils/internal/cmds"
	cmdsenv "github.com/djdv/go-filesystem-utils/internal/cmds/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/executor"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
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
			ctx = context.Background()
			// FIXME: the remote test commands are not going to be seen
			// when the executor respawns us (argv[0])
			// We need an explicit testRoot() that adds on to the normal root
			// root = cmdslib.Root()
			root = testRoot()
			err  = cli.Run(ctx, root, os.Args,
				os.Stdin, os.Stdout, os.Stderr,
				cmdsenv.MakeEnvironment, executor.MakeExecutor)
		)
		if err != nil {
			var cliError cli.ExitError
			if errors.As(err, &cliError) {
				os.Exit(int(cliError))
			}
		}
		os.Exit(0)
	}
	// Otherwise call Go's standard testing.Main
	os.Exit(m.Run())
}

const remoteCommandName = "remote-test"

type responseType bool

func testRoot() *cmds.Command {
	// responseInstance responseType // Pre-generic vestige - will be reflected for its type.
	root := cmdslib.Root()
	root.Subcommands[remoteCommandName] = &cmds.Command{
		NoLocal: true,
		// TODO: file a bug upstream
		// even if Type is non-pointer, http.Response gives us one.
		// This is inconsistent with chan.Response which gives us the exact value
		// Either chan should always return pointers, or http should return values
		// (if the type is a value rather than a pointer)
		// Type:    responseInstance,
		Type: new(responseType),
		Run: func(_ *cmds.Request, re cmds.ResponseEmitter, _ cmds.Environment) error {
			// if err := re.Emit(responseType(true)); err != nil {
			var response responseType = true
			if err := re.Emit(&response); err != nil {
				return err
			}
			return nil
		},
	}
	return root
}

func TestExecutor(t *testing.T) {
	t.Run("valid", testExecutorValid)
}

func testExecutorValid(t *testing.T) {
	t.Run("local", localCommand)
	t.Run("remote", remoteCommands)
}

func localCommand(t *testing.T) {
	var (
		ctx, cancel = context.WithCancel(context.Background())
		root        = &cmds.Command{
			Run: func(*cmds.Request, cmds.ResponseEmitter, cmds.Environment) error {
				return nil
			},
		}
		request, env            = newRequestAndEnv(t, ctx, ctx, root, nil, nil)
		exec, emitter, response = newExecAndResponsePair(t, env, request)
	)
	defer cancel()
	if err := exec.Execute(request, emitter, env); err != nil {
		t.Error(err)
	}
	respChan, errs := responseToChan[any](response)
	for respChan != nil ||
		errs != nil {
		select {
		case _, ok := <-respChan:
			if !ok {
				respChan = nil
				continue
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			t.Error(err)
			cancel()
		}
	}
}

// TODO: break the cmdslib dance into generic functions or something.
// We magically know how it works, but it's not obvious and not documented upstream either.
// Since we can use type parameters we can probably break this into generic steps or callbacks.
func remoteCommands(t *testing.T) {
	t.Run("auto daemon", autoDaemon)
	t.Run("manual daemon", manualDaemon)
}

func manualDaemon(t *testing.T) {
	var (
		daemonErrs  <-chan error
		ctx, cancel = context.WithCancel(context.Background())
		daemonOpts  = daemonOpts()
	)
	defer cancel()
	t.Run("start daemon", func(t *testing.T) { daemonErrs = execDaemon(t, ctx, daemonOpts) })
	t.Run("call remote", func(t *testing.T) { execRemote(t, ctx, daemonOpts) })
	t.Run("request daemon stop", func(t *testing.T) { execStop(t, ctx, daemonOpts) })
	t.Run("wait for daemon to stop", func(t *testing.T) {
		const timeout = 4 * time.Second
		select {
		case err := <-daemonErrs:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(timeout):
			t.Error("daemon did not stop after ", timeout)
		}
	})
}

func autoDaemon(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Run("call remote", func(t *testing.T) { execRemote(t, ctx, nil) })
	t.Run("request daemon stop", func(t *testing.T) { execStop(t, ctx, nil) })
}

func daemonOpts() cmds.OptMap {
	const sockName = "exec-test-socket"
	var (
		sockPath         = filepath.Join(os.TempDir(), sockName)
		sockMaddr        = "/unix/" + sockPath
		apiParameterName = settings.APIParam().Name(parameters.CommandLine)
	)
	return cmds.OptMap{apiParameterName: []string{sockMaddr}}
}

// returned context is done when daemon call exits.
func execDaemon(t *testing.T, ctx context.Context, options cmds.OptMap) <-chan error {
	var (
		root                 = testRoot()
		testPath             = []string{service.Name, daemon.Name}
		request, env         = newRequestAndEnv(t, ctx, ctx, root, testPath, options)
		_, emitter, response = newExecAndResponsePair(t, env, request)
	)
	go func() {
		root.Call(request, emitter, env)
	}()
	var (
		noopHandler      = func(*daemon.Response) error { return nil }
		startup, runtime = daemon.SplitResponse(response, noopHandler, noopHandler)
		daemonErrs       = make(chan error, 1)
	)
	if err := startup(); err != nil {
		t.Error(err)
	}
	go func() {
		defer close(daemonErrs)
		if err := runtime(); err != nil {
			daemonErrs <- err
		}
	}()

	return daemonErrs
}

func execRemote(t *testing.T, ctx context.Context, options cmds.OptMap) {
	var (
		root                                 = testRoot()
		testPath                             = []string{remoteCommandName}
		expectedResponse        responseType = true
		request                              = newRequest(t, ctx, ctx, root, testPath, options)
		exec, emitter, response              = newExecAndResponsePair(t, nil, request)
		execErrs                             = execute(exec, request, emitter, nil)
		respChan, respErrs                   = responseToChan[*responseType](response)
	)
	select {
	case err := <-execErrs:
		t.Fatal(err)
	default:
	}
	for respChan != nil ||
		respErrs != nil {
		select {
		case resp, ok := <-respChan:
			if !ok {
				respChan = nil
				continue
			}
			if *resp != expectedResponse {
				// TODO: add a count/bool we only expect to get 1 response, if we get more, error out.
				t.Errorf("command responded with unexpected response"+
					"\n\tgot: (%T)%v"+
					"\n\twant: (%T)%v",
					resp, resp, expectedResponse, expectedResponse,
				)
			}
		case err, ok := <-respErrs:
			if !ok {
				respErrs = nil
				continue
			}
			t.Error(err)
		}
	}

	if err := <-execErrs; err != nil {
		t.Error(err)
	}
}

func execStop(t *testing.T, ctx context.Context, options cmds.OptMap) {
	var (
		root                    = testRoot()
		testPath                = []string{service.Name, daemon.Name, stop.Name}
		request                 = newRequest(t, ctx, ctx, root, testPath, options)
		exec, emitter, response = newExecAndResponsePair(t, nil, request)
		execErrs                = execute(exec, request, emitter, nil)
		respChan, respErrs      = responseToChan[any](response)
	)
	select {
	case err := <-execErrs:
		t.Fatal(err)
	default:
	}
	for respChan != nil ||
		respErrs != nil {
		select {
		case _, ok := <-respChan:
			if !ok {
				respChan = nil
				continue
			}
		case err, ok := <-respErrs:
			if !ok {
				respErrs = nil
				continue
			}
			t.Error(err)
		}
	}
	if err := <-execErrs; err != nil {
		t.Error(err)
	}
}

func execute(exec cmds.Executor, request *cmds.Request,
	emitter cmds.ResponseEmitter, env cmds.Environment,
) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		if err := exec.Execute(request, emitter, env); err != nil {
			errs <- err
		}
	}()
	return errs
}

func responseToChan[responseType any](response cmds.Response) (<-chan responseType, <-chan error) {
	var (
		out  = make(chan responseType)
		errs = make(chan error)
	)
	go func() {
		defer close(out)
		defer close(errs)
		for {
			resp, err := response.Next()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					errs <- err
				}
				return
			}
			typedResponse, ok := resp.(responseType)
			if !ok {
				errs <- fmt.Errorf("emitter responded with unexpected type"+
					"\n\tgot: %T"+
					"\n\twant: %T",
					resp, typedResponse,
				)
				continue // Caller should cancel request/response context now.
			}
			out <- typedResponse
		}
	}()
	return out, errs
}

func stopIntstance(t *testing.T,
	serviceMaddr multiaddr.Multiaddr, root *cmds.Command,
	subCmd *exec.Cmd,
) {
	client, err := daemon.MakeClient(serviceMaddr)
	if err != nil {
		t.Fatal(err)
	}

	testCtx, testCancel := context.WithCancel(context.Background())
	defer testCancel()

	stopRequest, err := cmds.NewRequest(testCtx,
		[]string{service.Name, daemon.Name, stop.Name},
		nil, nil, nil, root,
	)
	if err != nil {
		t.Fatal(err)
	}

	// TODO: Better names.
	var (
		emitter, response = cmds.NewChanResponsePair(stopRequest)
		responseErrs      = make(chan error, 1)
		responseHandler   = func() {
			defer close(responseErrs)
			for {
				if _, err := response.Next(); err != nil {
					if !errors.Is(err, io.EOF) {
						responseErrs <- err
					}
					return
				}
			}
		}
		execErrs    = make(chan error, 1)
		execHandler = func() {
			defer close(execErrs)
			if err := client.Execute(stopRequest, emitter, nil); err != nil {
				execErrs <- err
			}
		}
	)
	go responseHandler()
	go execHandler()

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
		testCancel()
		t.Error(err)
	}

	if err := subCmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func newRequest(t *testing.T, reqCtx, envCtx context.Context,
	root *cmds.Command, path []string, optMap cmds.OptMap,
) *cmds.Request {
	t.Helper()
	request, err := cmds.NewRequest(reqCtx, path, optMap,
		nil, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func newRequestAndEnv(t *testing.T, reqCtx, envCtx context.Context,
	root *cmds.Command, path []string, optMap cmds.OptMap,
) (*cmds.Request, cmds.Environment) {
	t.Helper()
	request := newRequest(t, reqCtx, envCtx, root, path, optMap)
	serviceEnv, err := cmdsenv.MakeEnvironment(envCtx, request)
	if err != nil {
		t.Fatal(err)
	}
	return request, serviceEnv
}

func newExecAndResponsePair(t *testing.T, env cmds.Environment, request *cmds.Request) (cmds.Executor, cmds.ResponseEmitter, cmds.Response) {
	exec, err := executor.MakeExecutor(request, env)
	if err != nil {
		t.Fatal(err)
	}
	emitter, response := cmds.NewChanResponsePair(request)
	return exec, emitter, response
}
