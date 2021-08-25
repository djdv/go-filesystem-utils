package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/control"
	"github.com/djdv/go-filesystem-utils/cmd/service/status"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

const Name = ipc.ServiceCommandName

var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: ipc.ServiceDescription,
	},
	NoRemote: true,
	Run:      fileSystemServiceRun,
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatFileSystemService,
	},
	Options:  parameters.CmdsOptionsFrom((*Settings)(nil)),
	Encoders: cmds.Encoders,
	Type:     Response{},
	Subcommands: func() map[string]*cmds.Command {
		var (
			actions     = service.ControlAction[:]
			controls    = control.GenerateCommands(actions...)
			subcommands = make(map[string]*cmds.Command, len(controls)+1)
		)
		subcommands[status.Name] = status.Command
		for i, action := range actions {
			subcommands[action] = controls[i]
		}
		return subcommands
	}(),
}

type (
	ResponseStatus uint
	Response       struct {
		Status ResponseStatus `json:",omitempty"`
		// formats.Multiaddr?
		ListenerMaddr multiaddr.Multiaddr `json:",omitempty"`
		Info          string              `json:",omitempty"`
	}
)

const (
	_ ResponseStatus = iota
	Starting
	Ready
	Stopped
)

func fileSystemServiceRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	var (
		ctx             = request.Context
		settings        = new(Settings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
	)
	if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
		return err
	}
	fsEnv, err := ipc.CastEnvironment(env)
	if err != nil {
		return err
	}
	serviceConfig, err := fsEnv.ServiceConfig(request)
	if err != nil {
		return err
	}

	return runService(request.Context,
		emitter, env,
		&settings.Settings, serviceConfig,
	)
}

func runService(ctx context.Context,
	emitter cmds.ResponseEmitter, env cmds.Environment,
	settings *fscmds.Settings, serviceConfig *service.Config) error {
	var (
		serviceErrs   = make(chan error)
		serviceDaemon = newDaemon(ctx, settings, env)
	)
	defer close(serviceErrs)

	fileSystemService, err := service.New(serviceDaemon, serviceConfig)
	if err != nil {
		return err
	}

	if service.Interactive() {
		serviceDaemon.logger = newCmdsLogger(emitter)
		go func() {
			serviceErrs <- runInteractiveMode(ctx, fileSystemService, serviceDaemon)
		}()
	} else {
		logger, err := newServiceLogger(fileSystemService)
		if err != nil {
			return err
		}
		serviceDaemon.logger = logger
		go func() { serviceErrs <- fileSystemService.Run() }()
	}

	return <-serviceErrs
}

func runInteractiveMode(ctx context.Context, systemService service.Service, daemon *serviceDaemon) error {
	if startInitErr := daemon.Start(systemService); startInitErr != nil {
		return startInitErr
	}

	daemonContext, daemonCancel := signal.NotifyContext(ctx, os.Interrupt)
	defer daemonCancel()

	<-daemon.waitForRun(daemonContext).Done()

	if runErr := daemon.Stop(systemService); runErr != nil {
		return runErr
	}

	return daemonContext.Err()
}

func getServiceListeners(providedMaddrs ...multiaddr.Multiaddr) (serviceListeners []manet.Listener,
	serviceCleanup cleanupFunc, err error) {
	if service.Interactive() {
		return interactiveListeners(providedMaddrs...)
	}
	return systemListeners(providedMaddrs...)
}

// interactiveListeners returns a list of listeners,
// as well as a cleanup function which should be called
// after all listeners are no longer in use.
// If no multiaddrs are provided, platform specific default values will be used instead.
func interactiveListeners(providedMaddrs ...multiaddr.Multiaddr) (serviceListeners []manet.Listener,
	serviceCleanup cleanupFunc, err error) {
	if len(providedMaddrs) != 0 {
		// NOTE: if maddrs were provided, we expect the environment to be ready for the target(s).
		// We do not set up or destroy anything in this case. (`serviceCleanup` remains nil)
		serviceListeners, err = listen(providedMaddrs...)
		return
	}

	var localDefaults []multiaddr.Multiaddr
	if localDefaults, err = fscmds.UserServiceMaddrs(); err != nil {
		return
	}
	// First suggestion should be most local to the user that launched us.
	suggestedMaddr := localDefaults[0]

	unixSocketDir, hadUnixSocket := maybeGetUnixSocketDir(suggestedMaddr)
	if hadUnixSocket {
		// If it contains a Unix socket, make the parent directory for it
		// and allow it to be deleted when the caller is done with it.
		if err = os.MkdirAll(unixSocketDir, 0o775); err != nil {
			return
		}
		serviceCleanup = func() error { return os.Remove(unixSocketDir) }
	}

	serviceListeners, err = listen(suggestedMaddr)
	if err != nil && serviceCleanup != nil {
		if cErr := serviceCleanup(); cErr != nil {
			err = fmt.Errorf("%w - could not cleanup: %s", err, cErr)
		}
	}

	return
}

// maybeGetUnixSocketDir returns the parent directory
// of the first Unix domain socket within the multiaddr (if any).
func maybeGetUnixSocketDir(ma multiaddr.Multiaddr) (target string, hadUnixComponent bool) {
	multiaddr.ForEach(ma, func(comp multiaddr.Component) bool {
		if hadUnixComponent = comp.Protocol().Code == multiaddr.P_UNIX; hadUnixComponent {
			target = comp.Value()
			if runtime.GOOS == "windows" { // `/C:\path` -> `C:\path`
				target = strings.TrimPrefix(target, `/`)
			}
			target = filepath.Dir(target)
			return true
		}
		return false
	})
	return
}

func formatFileSystemService(response cmds.Response, emitter cmds.ResponseEmitter) error {
	var (
		request         = response.Request()
		ctx             = request.Context
		settings        = new(Settings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
	)
	if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
		return err
	}

	outputs := formats.MakeOptionalOutputs(response.Request(), emitter)

	for {
		untypedResponse, err := response.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			return nil
		}

		response, ok := untypedResponse.(*Response)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type+value: %#v", untypedResponse)
		}

		switch response.Status {
		case Starting:
			outputs.Print(ipc.StdHeader + "\n")
		case Ready:
			if response.ListenerMaddr != nil {
				outputs.Print(fmt.Sprintf("%s%s\n", ipc.StdGoodStatus, response.ListenerMaddr))
			} else {
				outputs.Print(ipc.StdReady + "\n")
				outputs.Print("Send interrupt to stop\n")
			}
		}

		if response.Info != "" {
			outputs.Print(response.Info + "\n")
		}

		outputs.Emit(response)
	}
}
