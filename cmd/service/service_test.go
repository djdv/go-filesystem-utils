package service_test

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"testing"
	"time"

	servicecmd "github.com/djdv/go-filesystem-utils/cmd/service"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/executor"
	fscmds "github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/environment"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
	"github.com/kardianos/service"
)

func TestMain(m *testing.M) {
	// When called with service arguments,
	// call the service's main function.
	if len(os.Args) >= 2 && os.Args[1] == servicecmd.Name {
		var (
			ctx  = context.Background()
			root = &cmds.Command{
				Options: fscmds.RootOptions(),
				Subcommands: map[string]*cmds.Command{
					servicecmd.Name: servicecmd.Command,
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
	// Otherwise call Go's standard `testing.Main`.
	os.Exit(m.Run())
}

type systemMock struct{ t *testing.T }

func (*systemMock) String() string    { return "go runtime" }
func (*systemMock) Detect() bool      { return true }
func (*systemMock) Interactive() bool { return false }
func (sm *systemMock) New(i service.Interface, c *service.Config) (service.Service, error) {
	return &serviceMock{t: sm.t, service: i, Config: c}, nil
}

type serviceMock struct {
	t       *testing.T
	service service.Interface
	*service.Config
	status service.Status
}

func (sm *serviceMock) Run() error {
	if err := sm.service.Start(sm); err != nil {
		return err
	}
	const runtimeMax = int(8 * time.Second)
	var (
		seed    = rand.NewSource(time.Now().UnixNano())
		rng     = rand.New(seed)
		runtime = time.Duration(rng.Intn(int(runtimeMax)))
	)
	<-time.After(runtime)
	return sm.service.Stop(sm)
}

func (sm *serviceMock) Start() error {
	if sm.status == service.StatusRunning {
		return errors.New("service already started")
	}
	sm.status = service.StatusRunning
	return nil
}

func (sm *serviceMock) Stop() error {
	if sm.status != service.StatusRunning {
		return errors.New("service was not started")
	}
	sm.status = service.StatusStopped
	return nil
}

func (sm *serviceMock) Restart() error {
	if err := sm.Stop(); err != nil {
		return err
	}
	return sm.Start()
}

func (sm *serviceMock) Install() error {
	sm.status = service.StatusStopped
	return nil
}

func (sm *serviceMock) Uninstall() error {
	sm.Stop()
	return nil
}

func (sm *serviceMock) Logger(errs chan<- error) (service.Logger, error) {
	return &loggerMock{t: sm.t}, nil
}

func (sm *serviceMock) SystemLogger(errs chan<- error) (service.Logger, error) {
	return sm.Logger(errs)
}

func (sm *serviceMock) String() string                  { return sm.Config.DisplayName }
func (*serviceMock) Platform() string                   { return "go runtime" }
func (sm *serviceMock) Status() (service.Status, error) { return sm.status, nil }

type loggerMock struct{ t *testing.T }

func (lm *loggerMock) Error(v ...interface{}) error   { lm.t.Error(v...); return nil }
func (lm *loggerMock) Warning(v ...interface{}) error { lm.t.Log(v...); return nil }
func (lm *loggerMock) Info(v ...interface{}) error    { lm.t.Log(v...); return nil }

func (lm *loggerMock) Errorf(format string, a ...interface{}) error {
	lm.t.Errorf(format, a...)
	return nil
}

func (lm *loggerMock) Warningf(format string, a ...interface{}) error {
	lm.t.Logf(format, a...)
	return nil
}

func (lm *loggerMock) Infof(format string, a ...interface{}) error {
	lm.t.Logf(format, a...)
	return nil
}

func TestServiceMocked(t *testing.T) {
	hostSystem := service.ChosenSystem()
	defer service.ChooseSystem(hostSystem)

	service.ChooseSystem(&systemMock{t: t})
	// TODO: run the same service tests as _real_rest.go
	// Except pass in a multiaddr argument
}
