package fscmds

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/adrg/xdg"
	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	config "github.com/ipfs/go-ipfs-config"
	"github.com/ipfs/go-ipfs/repo/fsrepo"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

const (
	serviceParameter   = "service"
	serviceDescription = "Manages active file system requests and instances"

	decayOptionKwd         = "decay" // TODO: name
	decayOptionDescription = "exit after N seconds if no instances"

	serviceSocketName = "filesystem.service"
	serviceStartGrace = 10 * time.Second
)

var Service = &cmds.Command{
	NoRemote:    true,
	Run:         filesystemRun,
	Encoders:    cmds.Encoders,
	Subcommands: make(map[string]*cmds.Command, len(service.ControlAction)),
	Options: []cmds.Option{
		cmds.StringOption(decayOptionKwd, decayOptionDescription).WithDefault("30s"),
	},
	Helptext: cmds.HelpText{
		LongDescription: serviceDescription,
	},
	/*
		PostRun: cmds.PostRunMap{
			cmds.CLI: mountPostRunCLI,
		},
	*/
}

var serviceConfigTemplate = service.Config{
	Name:        "FileSystemDaemon",
	DisplayName: "File System Daemon",
	Description: serviceDescription,
}

func init() { registerServiceSubcommands(Service) }

func registerServiceSubcommands(parent *cmds.Command) {
	for _, action := range service.ControlAction {
		actionStr := action
		parent.Subcommands[action] = &cmds.Command{
			Run: func(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) (err error) {
				fsEnv, envIsUsable := env.(FileSystemEnvironment)
				if !envIsUsable {
					return envError(env)
				}

				svc, err := getService(request, fsEnv)
				if err != nil {
					return err
				}

				// TODO: special case for "status"
				// we want to display both the system service manager status
				// and the status of the actual file system service itself
				// (e.g. is the service regisetred and running, + is the socket dialable)
				/*output:
				Service API: Listening - /unix/run/fsd.sock
				Service Manager: Running - File System Service Daemon
				---
				Service API: Not listening
				Service Manager: Not installed
				*/
				return service.Control(svc, actionStr)
			},
		}
	}
}

type daemon struct {
	FileSystemEnvironment
	serviceListener manet.Listener
}

// TODO: better way to signal "ready"
// previously we used both stdout and stderr
// but the service logger we use now, uses stderr exclusively
// maybe override this during construction in main
// and go back to line from stdout == okay, line from stderr == not okay
//
// we promise to print this sequence to the service pipe during normal operation
// anything else should be considered an error
const (
	stdHeader     = "Starting"
	stdReady      = "Started"
	stdGoodStatus = "Listening on: "
)

func (d *daemon) Start(s service.Service) error {
	logger, err := s.Logger(nil)
	if err != nil {
		return err
	}

	logger.Info(stdHeader)

	if d.serviceListener != nil {
		err = fmt.Errorf("service already listening on: %s",
			d.serviceListener.Multiaddr().String())
		logger.Error(err)
		return err
	}

	serviceMaddr := d.FileSystemEnvironment.ServiceMaddr()
	if err := maybeRemoveUnixSockets(serviceMaddr); err != nil {
		logger.Error(err)
		return err // this filters "not-found" errors, so these are fatal only
	}
	if err = makeServiceDir(serviceMaddr); err != nil {
		logger.Errorf("could not create service directory: %s", err)
		return err
	}
	serviceListener, err := manet.Listen(serviceMaddr)
	if err != nil {
		logger.Errorf("could not start listener: %s", err)
		return err
	}

	d.serviceListener = serviceListener
	logger.Info(stdGoodStatus, serviceMaddr)

	go http.Serve(manet.NetListener(serviceListener),
		cmdshttp.NewHandler(d.FileSystemEnvironment, ClientRoot, cmdshttp.NewServerConfig()))

	return logger.Info(stdReady)
}

func (d *daemon) Stop(s service.Service) (err error) {
	var logger service.Logger
	if logger, err = s.Logger(nil); err != nil {
		return
	}

	if d.serviceListener == nil {
		err = fmt.Errorf("service was not started / is not listening")
		logger.Error(err)
		return
	}
	serviceMaddr := d.serviceListener.Multiaddr()
	logger.Info("closing listener: ", serviceMaddr.String())

	// TODO: close all instances first

	if err = d.serviceListener.Close(); err != nil {
		logger.Error("listener encountered error: ", err)
		// non-fatal error
	}
	d.serviceListener = nil

	socketTarget, mErr := serviceMaddr.ValueForProtocol(multiaddr.P_UNIX)
	switch mErr {
	case nil:
		socketTarget = strings.TrimPrefix(socketTarget, `/`)
		// cleanup system service directory (should be empty post-close)
		if oErr := os.Remove(filepath.Dir(socketTarget)); oErr != nil {
			oErr = fmt.Errorf("failed to cleanup service directory: %w", oErr)
			logger.Error(oErr)
			if err != nil {
				err = fmt.Errorf("%w; %s", err, oErr)
			} else {
				err = oErr
			}
		}
	default:
	}
	return
}

func multiaddrOption(request *cmds.Request, parameter string) (multiaddr.Multiaddr, error) {
	if apiArg, provided := request.Options[parameter]; provided {
		api, isString := apiArg.(string)
		if !isString {
			return nil, cmds.Errorf(cmds.ErrClient,
				"%s's argument %v is type: %T, expecting type: %T",
				parameter, apiArg, apiArg, api)
		}
		return multiaddr.NewMultiaddr(api)
	}
	return nil, nil
}

// TODO: prerun with defaults, run uses arg vector
func filesystemRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	fsEnv, envIsUsable := env.(FileSystemEnvironment)
	if !envIsUsable {
		return envError(env)
	}

	service, err := getService(request, fsEnv)
	if err != nil {
		return err
	}

	// TODO:
	// quick hacks just to prevent the process staying around forever
	// this needs to be done properly
	decay, ok := request.Options[decayOptionKwd]
	if ok {
		interval, err := time.ParseDuration(decay)
		index, err := fsEnv.Index(request)
		if err != nil {
			panic(err)
		}
		go func() {
			for {
				select {
				case <-time.After(time.Second * time.Duration(decay.(int))): // XXX: no
					isEmpty := true
					for range index.List(request.Context) {
						isEmpty = false
					}
					if isEmpty {
						service.Stop()
						os.Exit(0)
					}
				}
			}
		}()
	}

	return service.Run()

	/* TODO: only on interactive:
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		// TODO: interrupt calls graceful stop,
		// double interrupt calls ungraceful
		<-interrupt
		// TODO: graceful server stop
		log.Println("interrupt received, closing: ", serviceListener.Addr().String())
		log.Println("closed: ", serviceListener.Close())
	}()
	*/

}

func getService(request *cmds.Request, fsEnv FileSystemEnvironment) (service.Service, error) {
	var (
		fileSystemService = &daemon{
			FileSystemEnvironment: fsEnv,
		}
	)

	serviceConfig := serviceConfigTemplate
	serviceConfig.Arguments = []string{serviceParameter}

	// TODO: move to platform constrained files
	if runtime.GOOS == "windows" {
		// TODO: see if we can get away with less privledged via LocalService or NetWorkService
		// we don't want SYSTEM level privledges
		// ^ we can at least drop capabilities on service.Start()
		//serviceConfig.UserName = `NT AUTHORITY\LocalService`
		// ^ needs sid to string conversion anyway
		// for now leave empty, defaults to NT AUTHORITY\LocalSystem
	} else {
		if service.Interactive() {
			user, err := user.Current()
			if err != nil {
				return nil, err
			}
			serviceConfig.UserName = user.Username
		} else {
			// leave null, let service lib decide which service user to use
		}
	}
	return service.New(fileSystemService, &serviceConfig)
}

func localServiceMaddr() (multiaddr.Multiaddr, error) {
	ourName := filepath.Base(os.Args[0])
	serviceName := strings.TrimSuffix(ourName, filepath.Ext(ourName))
	// use existing socket if found
	socketName := filepath.Join(serviceName, serviceSocketName)
	servicePath, err := xdg.SearchRuntimeFile(socketName)
	if err != nil {
		servicePath, err = xdg.SearchConfigFile(socketName)
	}
	if err != nil { // otherwise use a default
		servicePath = filepath.Join(localServiceDirectory(), serviceSocketName)
	}

	return multiaddr.NewMultiaddr(path.Join("/unix/", filepath.ToSlash(servicePath)))
}

func localServiceDirectory() string {
	ourName := filepath.Base(os.Args[0])
	serviceName := strings.TrimSuffix(ourName, filepath.Ext(ourName))
	if service.Interactive() {
		// (for interactive, use the most user-specific path)
		return filepath.Join(xdg.RuntimeDir, serviceName)
	}
	// (for system services, use the least user-specific path)
	// NOTE: xdg standard says this list should always have a fallback
	// (so this list should never contain less than 1 element - regardless of platform)
	return filepath.Join(xdg.ConfigDirs[len(xdg.ConfigDirs)-1], serviceName)
}

// maybeRemoveUnixSockets attempts to remove all Unix domain socket paths in a given multiaddr.
// It returns all errors encountered (wrapped), excluding "not exist" errors.
func maybeRemoveUnixSockets(ma multiaddr.Multiaddr) (err error) {
	multiaddr.ForEach(ma, func(comp multiaddr.Component) bool {
		if comp.Protocol().Code == multiaddr.P_UNIX {
			localPath := comp.Value()
			if runtime.GOOS == "windows" { // `/C:\path` -> `C:\path`
				localPath = strings.TrimPrefix(localPath, `/`)
			}
			osErr := os.Remove(localPath)
			if osErr != nil && !os.IsNotExist(osErr) {
				if err == nil {
					err = fmt.Errorf("remove socket error: %w", osErr)
				} else {
					err = fmt.Errorf("%w, %s", err, osErr)
				}
			}
		}
		return false
	})
	return
}

func getIPFSAPIAddr(request *cmds.Request) (multiaddr.Multiaddr, error) {
	// (precedence 0) command line flags
	apiAddr, err := multiaddrOption(request, rootIPFSOptionKwd)
	if err != nil {
		return nil, err
	}
	if apiAddr != nil {
		return apiAddr, nil
	}

	// TODO: (precedence 1) environment variable
	// ${IPFS_API}

	// (precedence 2) persistent storage
	confRoot, err := config.PathRoot()
	if err == nil {
		apiAddr, err = fsrepo.APIAddr(confRoot)
	}
	return apiAddr, err
}

<<<<<<< Updated upstream
func resolveAddr(ctx context.Context, addr multiaddr.Multiaddr) (multiaddr.Multiaddr, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, 10*time.Second)
	defer cancelFunc()

	addrs, err := madns.DefaultResolver.Resolve(ctx, addr)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		return nil, errors.New("non-resolvable API endpoint")
	}

	return addrs[0], nil
}

func relaunchSelfAsService(request *cmds.Request, serviceMaddr multiaddr.Multiaddr) (*int, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(self, serviceParameter,
		fmt.Sprintf("--%s=%s", rootServiceOptionKwd, serviceMaddr.String()),
		fmt.Sprintf("--%s=%s", decayOptionKwd, "30"),
	)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	servicePipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}

	releasedPid := cmd.Process.Pid
	if err = cmd.Process.Release(); err != nil {
		return nil, err
	}

	err = waitForService(servicePipe, serviceStartGrace)
	if err != nil {
		attachedProc, pErr := os.FindProcess(releasedPid)
		if pErr == nil {
			kErr := attachedProc.Kill()
			if kErr != nil {
				return &releasedPid, fmt.Errorf("%w - additionally could not kill process: %s", err, kErr)
			}
		}
		return nil, fmt.Errorf("could not start background service: %w", err)
	}

	return &releasedPid, servicePipe.Close()
}

// waitForService scans the input,
// and waits until the provided maddr is available (or times out).
func waitForService(input io.Reader, timeout time.Duration) error {
	var (
		serviceScanner = bufio.NewScanner(input)
		scannerErr     = make(chan error, 1)
		timeoutChan    <-chan time.Time
	)
	go func() {
		defer close(scannerErr)
		serviceScanner.Scan()
		{
			text := serviceScanner.Text()
			if !strings.Contains(text, stdHeader) {
				scannerErr <- fmt.Errorf("unexpected process output: %s", text)
				return
			}
		}

		var text string
		for serviceScanner.Scan() {
			text = serviceScanner.Text()
			if strings.Contains(text, stdReady) {
				return
			}
		}
		scannerErr <- fmt.Errorf("process output ended abruptly: %s", text)
	}()

	if timeout > 0 {
		timeoutChan = time.After(timeout)
	}
	select {
	case <-timeoutChan:
		return fmt.Errorf("timed out")
	case err := <-scannerErr:
		return err
	}
}
