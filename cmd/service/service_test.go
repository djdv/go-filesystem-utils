package service_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	"github.com/djdv/go-filesystem-utils/cmd/service/status"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
	hostservice "github.com/kardianos/service"
)

type cmdsResponse struct {
	value interface{}
	error
}

func TestServiceRun(t *testing.T) {
	callServiceMethod := func(ctx context.Context, optMap cmds.OptMap) (<-chan error, context.CancelFunc, error) {
		request, err := cmds.NewRequest(ctx, []string{service.Name},
			optMap, nil, nil, testRoot)
		if err != nil {
			return nil, nil, err
		}
		environment, err := service.MakeEnvironment(ctx, request)
		if err != nil {
			return nil, nil, err
		}
		executor, err := service.MakeExecutor(request, environment)
		if err != nil {
			return nil, nil, err
		}

		// HACK:
		// chanresponse's emitter will race between
		// its request's context and Run's emits.
		// We give it our own context to assure it returns
		// the value from service.Run.
		var (
			testContext, testCancel = context.WithCancel(context.Background())
			testRequest             = *request

			execChan = make(chan error, 1)
			respChan = make(chan error, 1)
			testChan = make(chan error, 1)
		)
		testRequest.Context = testContext

		emitter, response := cmds.NewChanResponsePair(&testRequest)
		go func() {
			execChan <- executor.Execute(request, emitter, environment)
		}()
		go func() { _, err := response.Next(); respChan <- err }()
		go func() {
			defer close(testChan)
			for execChan != nil ||
				respChan != nil {
				select {
				case <-testContext.Done():
					testChan <- fmt.Errorf("test canceled before service returned")
					return
				case execErr := <-execChan:
					if err != nil {
						testChan <- fmt.Errorf("failed to execute service command: %w",
							execErr)
					}
					execChan = nil
				case responseErr := <-respChan:
					expectedErr := io.EOF
					if !errors.Is(responseErr, expectedErr) {
						testChan <- fmt.Errorf("service run failed\n\texpected %v\n\tgot: %w",
							expectedErr, responseErr)
					}
					respChan = nil
				}
			}
		}()

		return testChan, testCancel, nil
	}

	t.Run("Cancel", func(t *testing.T) {
		var (
			testCtx     = context.Background()
			cancelCheck = func(t *testing.T, serviceChan <-chan error, expectedError error) {
				t.Helper()
				serviceErr := <-serviceChan
				if serviceErr == nil {
					t.Fatalf("expected service to be canceled but no error was returned")
				}
				if !errors.Is(serviceErr, expectedError) {
					t.Fatalf("service returned unexpected error\n\twanted: %s\n\tgot: %s",
						expectedError, serviceErr)
				}
			}
		)
		t.Run("Context", func(t *testing.T) {
			t.Run("Early", func(t *testing.T) {
				runCtx, runCancel := context.WithCancel(testCtx)
				runCancel()

				serviceErr, testCancel, err := callServiceMethod(runCtx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer testCancel()

				cancelCheck(t, serviceErr, context.Canceled)
			})
			t.Run("Late", func(t *testing.T) {
				const serviceWait = time.Microsecond
				runCtx, runCancel := context.WithCancel(testCtx)
				go func() {
					time.Sleep(serviceWait)
					runCancel()
				}()

				serviceErr, testCancel, err := callServiceMethod(runCtx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer testCancel()

				cancelCheck(t, serviceErr, context.Canceled)
			})
		})
		t.Run("Auto shutdown", func(t *testing.T) {
			const (
				stopAfter = time.Nanosecond
				testGrace = stopAfter + (10 * time.Second)
			)

			runCtx, runCancel := context.WithCancel(testCtx)
			defer runCancel()
			serviceErr, testCancel, err := callServiceMethod(runCtx, cmds.OptMap{
				fscmds.AutoExitParameter.Name: stopAfter.String(),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer testCancel()

			select {
			case <-time.After(testGrace):
				testCancel()
				err = <-serviceErr
				t.Fatal("service process did not stop in time: ", err)
			case err = <-serviceErr:
				expectedError := context.DeadlineExceeded
				switch {
				case err == nil:
					t.Fatalf("expected service to be canceled but no error was returned")
				case !errors.Is(err, expectedError):
					t.Fatalf("service returned unexpected error\n\twanted: %s\n\tgot: %s",
						expectedError, err)
				}
			}
		})
	})
}

// waitForService queries the service status a few times, but eventually fails.
//
// When launching the service in the background,
// tests may want to wait for it to start listening before proceeding.
func waitForService(t *testing.T) error {
	const (
		checkInterval = 100 * time.Millisecond
		checkTimeout  = 10 * time.Second
	)
	var (
		ticker  = time.NewTicker(checkInterval)
		timeout = time.NewTimer(checkTimeout)
	)
	defer func() {
		ticker.Stop()
		timeout.Stop()
	}()
	for {
		t.Logf("waiting %s for service to start...", checkInterval)
		select {
		case <-ticker.C:
			serviceStatus, err := issueStatusRequest()
			if err == nil &&
				serviceStatus.DaemonListener != nil {
				return nil
			}
		case <-timeout.C:
			return fmt.Errorf("service did not start in time (%s)", checkTimeout)
		}
	}
}

func issueControlRequest(controlAction string) error {
	var (
		ctx              = context.Background()
		ourName          = filepath.Base(os.Args[0])
		serviceArguments = []string{ourName, service.Name}
		discard, err     = os.OpenFile(os.DevNull, os.O_RDWR, 0o755)
	)
	if err != nil {
		return err
	}
	defer discard.Close()
	return cli.Run(ctx, testRoot,
		append(serviceArguments, controlAction),
		discard, discard, discard,
		service.MakeEnvironment, service.MakeExecutor)
}

// The service uninstall control may return
// before the system service is fully stopped and deleted.
// As such, we want to explicitly wait between tests
// until the service control manager finishes removing the service,
// or we give up.
func waitForUninstall(t *testing.T) {
	t.Helper()

	const (
		checkInterval = 100 * time.Millisecond
		checkTimeout  = 10 * time.Second
	)
	var (
		ticker  = time.NewTicker(checkInterval)
		timeout = time.NewTimer(checkTimeout)
	)
	defer func() {
		ticker.Stop()
		timeout.Stop()
	}()
	for {
		t.Logf("waiting %s for uninstall...", checkInterval)
		select {
		case <-ticker.C:
			serviceStatus, err := issueStatusRequest()
			if err != nil {
				t.Fatal(err)
			}
			if svcErr := serviceStatus.ControllerError; svcErr != nil &&
				errors.Is(svcErr, hostservice.ErrNotInstalled) {
				return
			}
		case <-timeout.C:
			t.Fatalf("uninstall control did not finish in time (%s)", checkTimeout)
		}
	}
}

func TestServiceControl(t *testing.T) {
	t.Run("bad sequence", func(t *testing.T) {
		for _, test := range []struct {
			controlAction string
			shouldError   bool
		}{
			{"invalid control action", true},
			{"uninstall", true},
			{"install", false},
			{"start", false},
			{"start", true},
			{"stop", false},
			{"stop", true},
			{"uninstall", false},
		} {
			var (
				controlAction = test.controlAction
				shouldError   = test.shouldError
			)
			t.Run(controlAction, func(t *testing.T) {
				err := issueControlRequest(controlAction)
				if shouldError &&
					err == nil {
					t.Errorf("control \"%s\" was supposed to return an error, but did not",
						controlAction)
				}
				if !shouldError &&
					err != nil {
					t.Errorf("control \"%s\" returned error: %s",
						controlAction, err)
				}
			})
		}
		waitForUninstall(t)
	})
	t.Run("good sequence", func(t *testing.T) {
		for _, testControl := range []string{
			"install",
			"start",
			"restart",
			"stop",
			"uninstall",
		} {
			controlAction := testControl
			t.Run(controlAction, func(t *testing.T) {
				if err := issueControlRequest(controlAction); err != nil {
					t.Error(err)
				}
			})
		}
		waitForUninstall(t)
	})
}

func issueStatusRequest() (*status.Status, error) {
	ctx := context.Background()
	statusRequest, err := cmds.NewRequest(ctx, []string{service.Name, "status"},
		nil, nil, nil, testRoot)
	if err != nil {
		return nil, err
	}

	environment, err := service.MakeEnvironment(ctx, statusRequest)
	if err != nil {
		return nil, err
	}
	executor, err := service.MakeExecutor(statusRequest, environment)
	if err != nil {
		return nil, err
	}

	var (
		emitter, response = cmds.NewChanResponsePair(statusRequest)
		respChan          = make(chan cmdsResponse, 1)
	)
	go func() {
		value, err := response.Next()
		respChan <- cmdsResponse{value: value, error: err}
	}()

	if err := executor.Execute(statusRequest, emitter, environment); err != nil {
		return nil, err
	}

	resp := <-respChan
	v, err := resp.value, resp.error
	if err != nil {
		return nil, err
	}

	serviceStatus, ok := v.(*status.Status)
	if !ok {
		return nil, fmt.Errorf("status value is wrong type\n\texpected:%T\n\tgot:%T %v",
			serviceStatus, v, v)
	}
	return serviceStatus, nil
}

func TestServiceStatus(t *testing.T) {
	// FIXME [linux/systemd:
	// systemd service always return "inactive"/"stopped" when not-installed
	// https://github.com/kardianos/service/issues/159
	// https://github.com/kardianos/service/issues/201
	t.Run("check if installed", func(t *testing.T) {
		serviceStatus, err := issueStatusRequest()
		if err != nil {
			t.Fatal(err)
		}
		var (
			svcErr        = serviceStatus.ControllerError
			expectedError = hostservice.ErrNotInstalled
		)
		if svcErr == nil ||
			!errors.Is(svcErr, expectedError) {
			t.Fatalf("expected serviceStatus to return error:\n\t%s\nbut got:\n\t%v",
				expectedError, err)
		}
	})
	t.Run("status sequence", func(t *testing.T) {
		for _, test := range []struct {
			controlAction  string
			expectedStatus hostservice.Status
		}{
			{
				"install",
				hostservice.StatusStopped,
			},
			{
				"start",
				hostservice.StatusRunning,
			},
			{
				"restart",
				hostservice.StatusRunning,
			},
			{
				"stop",
				hostservice.StatusStopped,
			},
		} {
			var (
				controlAction  = test.controlAction
				expectedStatus = test.expectedStatus
			)
			t.Run(controlAction, func(t *testing.T) {
				err := issueControlRequest(controlAction)
				if err != nil {
					t.Error(err)
				}
				serviceStatus, err := issueStatusRequest()
				if err != nil {
					t.Fatal(err)
				}
				if serviceStatus.ControllerStatus != expectedStatus {
					t.Errorf("service state is not what we expected\n\twanted: %v\n\tgot: %v",
						expectedStatus, serviceStatus.ControllerStatus)
				}
			})
		}
	})
	t.Run("uninstall test service", func(t *testing.T) {
		if err := issueControlRequest("uninstall"); err != nil {
			t.Error(err)
		}
		waitForUninstall(t)
	})
}

func TestServiceFormat(t *testing.T) {
	var (
		ctx          = context.Background()
		ourName      = filepath.Base(os.Args[0])
		discard, err = os.OpenFile(os.DevNull, os.O_RDWR, 0o755)
	)
	if err != nil {
		t.Fatal(err)
	}
	defer discard.Close()

	for _, encoding := range []string{
		cmds.JSON,
		cmds.XML,
		cmds.Text,
		cmds.TextNewline,
	} {
		tests := []struct {
			cmdsPath []string
			encoding string
		}{
			{[]string{service.Name, "status"}, encoding},
			{[]string{service.Name, "install"}, encoding},
			{[]string{service.Name, "status"}, encoding},
			{[]string{service.Name, "start"}, encoding},
			{[]string{service.Name, "status"}, encoding},
			{[]string{service.Name, "stop"}, encoding},
			{[]string{service.Name, "uninstall"}, encoding},
		}
		var lastErr error
		t.Run(encoding, func(t *testing.T) {
			for _, test := range tests {
				cmdline := append([]string{
					ourName,
					fmt.Sprintf("--%s=%s", cmds.EncLong, test.encoding),
				}, test.cmdsPath...)
				t.Run(strings.Join(cmdline, " "), func(t *testing.T) {
					// NOTE:
					// We're not testing the command/sequence correctness,
					// just the formatting. Including expected errors,
					// and coverage for panics and hangs in the format code.
					lastErr = cli.Run(ctx, testRoot, cmdline,
						discard, discard, discard,
						service.MakeEnvironment, service.MakeExecutor)
				})
			}
		})
		if lastErr == nil {
			waitForUninstall(t)
		}
	}
}
