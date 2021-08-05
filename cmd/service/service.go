package service

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
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
	NoRemote: true,
	Run:      fileSystemServiceRun,
	Options:  parameters.CmdsOptionsFrom((*Settings)(nil)),
	Encoders: cmds.Encoders,
	Helptext: cmds.HelpText{
		Tagline: ipc.ServiceDescription,
	},
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

func fileSystemServiceRun(request *cmds.Request, _ cmds.ResponseEmitter, env cmds.Environment) error {
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

	serviceDaemon := newDaemon(ctx, &settings.Settings, env)

	return runService(request.Context, serviceConfig, serviceDaemon)
}

func runService(ctx context.Context, config *service.Config, daemon *serviceDaemon) error {
	fileSystemService, err := service.New(daemon, config)
	if err != nil {
		return err
	}
	if service.Interactive() {
		return runInteractiveMode(ctx, fileSystemService, daemon)
	}
	return runServiceMode(ctx, fileSystemService)
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

func runServiceMode(ctx context.Context, systemService service.Service) error {
	serviceChan := make(chan error)
	go func() { serviceChan <- systemService.Run() }()

	select {
	case runErr := <-serviceChan:
		// service Run returned, likely from a Stop request
		return runErr
	case <-ctx.Done():
		// Run has not returned, but we're canceled.
		// Issue the stop control ourselves.
		if stopErr := systemService.Stop(); stopErr != nil {
			return fmt.Errorf("could not stop service process: %w", stopErr)
		}

		// NOTE [unstoppable]:
		// If this blocks forever, it's an implementation error.
		// However, if Run doesn't return soon,
		// the service manager will kill the process anyway. ☠
		if runErr := <-serviceChan; runErr != nil {
			return runErr
		}

		return ctx.Err()
	}
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
