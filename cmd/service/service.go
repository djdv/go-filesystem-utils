package service

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

const (
	Name        = "service"
	description = "Manages active file system requests and instances."

	serviceDisplayName = "File System Daemon"
	serviceName        = "FileSystemDaemon"
	startGrace         = 10 * time.Second
	stopGrace          = 30 * time.Second

	// NOTE: Used by the executor.
	// To synchronize with a service subprocess that outputs to StdErr.
	stdHeader     = serviceDisplayName + " starting..."
	stdGoodStatus = "Listening on: "
	stdReady      = serviceDisplayName + " started"
)

var Command = &cmds.Command{
	NoRemote: true,
	Run:      fileSystemServiceRun,
	Options:  parameters.CmdsOptionsFrom((*Settings)(nil)),
	Encoders: cmds.Encoders,
	Helptext: cmds.HelpText{
		Tagline: description,
	},
	Subcommands: generateServiceSubcommands(),
}

func fileSystemServiceRun(request *cmds.Request, _ cmds.ResponseEmitter, env cmds.Environment) error {
	var (
		ctx             = request.Context
		settings        = new(Settings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
		_, err = parameters.AccumulateArgs(ctx, unsetArgs, errs)
	)
	if err != nil {
		return err
	}

	serviceConfig := &service.Config{
		Name:        serviceName,
		DisplayName: serviceDisplayName,
		Description: description,
		UserName:    settings.Username,
		Option:      serviceKeyValueFrom(&settings.PlatformSettings),
		Arguments:   serviceArgs(settings),
	}

	serviceDaemon := newDaemon(ctx, &settings.Settings, env)

	return runService(request.Context, serviceConfig, serviceDaemon)
}

func serviceArgs(settings *Settings) (serviceArgs []string) {
	serviceArgs = []string{Name}
	if len(settings.ServiceMaddrs) > 0 {
		// Copy service-relevant arguments from our process,
		// into the service config. The service manager will
		// use these when starting its own process.
		apiParam := fscmds.ServiceMaddrs().CommandLine()
		for _, arg := range os.Args {
			if strings.HasPrefix(
				strings.TrimLeft(arg, "-"),
				apiParam,
			) {
				serviceArgs = append(serviceArgs, arg)
			}
		}
	}
	if settings.AutoExitInterval != 0 {
		exitParam := fscmds.AutoExitInterval().CommandLine()
		for _, arg := range os.Args {
			if strings.HasPrefix(arg, exitParam) {
				serviceArgs = append(serviceArgs, arg)
			}
		}
	}
	return serviceArgs
}

// NOTE: Field names and data types in the setting's struct declaration
// must match the map key names defined in the `service.KeyValue` pkg documentation.
func serviceKeyValueFrom(platformSettings *PlatformSettings) service.KeyValue {
	var (
		settingsValue   = reflect.ValueOf(platformSettings).Elem()
		settingsType    = settingsValue.Type()
		settingsCount   = settingsType.NumField()
		serviceSettings = make(service.KeyValue, settingsCount)
	)
	for i := 0; i != settingsCount; i++ {
		structField := settingsType.Field(i) // The field itself (for its name).
		fieldValue := settingsValue.Field(i) // The value it holds (not its type name).
		serviceSettings[structField.Name] = fieldValue.Interface()
	}
	return serviceSettings
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
