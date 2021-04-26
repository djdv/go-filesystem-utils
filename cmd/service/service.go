package service

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
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

var (
	UsernameParameter = fscmds.CmdsParameterSet{
		Name:        "username",
		Description: "Username to use when interfacing with the system service manager.",
		Environment: "FS_SERVICE_USERNAME",
	}

	Command = &cmds.Command{
		NoRemote: true,
		Run:      fileSystemServiceRun,
		Options: append([]cmds.Option{
			cmds.StringOption(UsernameParameter.Name, UsernameParameter.Description),
		},
			generatePlatformOptions(servicePlatformOptions)...),
		Encoders: cmds.Encoders,
		Helptext: cmds.HelpText{
			Tagline: description,
		},
		Subcommands: generateServiceSubcommands(),
	}
)

func fileSystemServiceRun(request *cmds.Request, _ cmds.ResponseEmitter, env cmds.Environment) error {
	serviceConfig, err := getHostServiceConfig(request)
	if err != nil {
		return fmt.Errorf("get service config: %w", err)
	}

	serviceDaemon, err := daemonInit(request, env)
	if err != nil {
		return maybeSyslogErr(serviceConfig, err)
	}

	return maybeSyslogErr(serviceConfig,
		runService(request.Context, serviceConfig, serviceDaemon))
}

func daemonInit(request *cmds.Request, env cmds.Environment) (*serviceDaemon, error) {
	daemonOptions, err := getDaemonArguments(request)
	if err != nil {
		return nil, err
	}

	serviceDaemon, err := newDaemon(request, env, daemonOptions...)
	if err != nil {
		return nil, err
	}

	return serviceDaemon, nil
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
	startInitErr := daemon.Start(systemService)
	if startInitErr != nil {
		return startInitErr
	}

	daemonContext, daemonCancel := signal.NotifyContext(ctx, os.Interrupt)
	defer daemonCancel()

	<-daemon.waitForRun(daemonContext).Done()

	runErr := daemon.Stop(systemService)
	if runErr != nil {
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
		// Run must return now,
		// if this communication blocks for too long
		// we'll be killed by the service manager. ☠
		runErr := <-serviceChan
		if runErr != nil {
			return runErr
		}

		return ctx.Err()
	}
}

// maybeSyslogErr conditionally logs the passed in error
// to the service logger defined in the service config.
// Returning the passed in error(, wrapped in logging errors if encountered).
func maybeSyslogErr(serviceConfig *service.Config, err error) error {
	if err == nil {
		return nil
	}

	if service.Interactive() {
		// Let the error be printed to Stderr by its the caller.
		// Not us.
		return err
	}

	// In service mode, use a service controller to get the system logger.
	// And log the error before returning it.
	serviceClient, sErr := service.New((*controller)(nil), serviceConfig)
	if sErr != nil {
		return fmt.Errorf("%w - %s",
			err, sErr)
	}

	if logger, lErr := serviceClient.SystemLogger(nil); lErr == nil {
		if lErr := logger.Error(err); lErr != nil {
			return fmt.Errorf("%w - %s",
				err, lErr)
		}
	}
	return err
}

// localServiceMaddr returns a maddr that's ready to listen,
// as well as a cleanup function that should be called right before the service stops.
func localServiceMaddr() (multiaddr.Multiaddr, cleanupFunc, error) {
	var (
		servicePath string
		cleanup     cleanupFunc
		err         error
	)
	if service.Interactive() {
		servicePath, cleanup, err = prepareInteractiveSocket()
	} else {
		servicePath, cleanup, err = prepareSystemSocket()
	}
	if err != nil {
		return nil, nil, err
	}

	serviceMaddr, err := multiaddr.NewMultiaddr(path.Join("/unix/",
		filepath.ToSlash(servicePath)))
	return serviceMaddr, cleanup, err
}

func prepareInteractiveSocket() (string, cleanupFunc, error) {
	var (
		xdgName = filepath.Join(fscmds.ServiceName, fscmds.ServerName)
		xdgErr  error
	)
	for _, constructor := range []func(string) (string, error){
		xdg.RuntimeFile,
		xdg.StateFile,
	} {
		servicePath, err := constructor(xdgName)
		if err != nil {
			if xdgErr == nil {
				xdgErr = err
			} else {
				xdgErr = fmt.Errorf("%s, %s", xdgErr, err)
			}
			continue
		}
		var cleanup cleanupFunc = func() error {
			return os.Remove(filepath.Dir(servicePath))
		}
		return servicePath, cleanup, err
	}
	return "", nil, xdgErr
}
