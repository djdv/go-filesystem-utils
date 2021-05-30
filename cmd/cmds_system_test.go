//go:build system
// +build system

package fscmds_test

import (
	"context"
	"errors"
	"fmt"
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

var testRoot = &cmds.Command{
	Options: fscmds.RootOptions(),
	Subcommands: map[string]*cmds.Command{
		service.Name: service.Command,
	},
}

func TestMain(m *testing.M) {
	// When called with service arguments,
	// call the service's main function.
	if len(os.Args) >= 2 && os.Args[1] == service.Name {
		var (
			ctx = context.Background()
			err = cli.Run(ctx, testRoot, os.Args,
				os.Stdin, os.Stdout, os.Stderr,
				service.MakeEnvironment, service.MakeExecutor)
		)
		if err != nil {
			cliError := new(cli.ExitError)
			if errors.As(err, cliError) {
				os.Exit(int(*cliError))
			}
		}
		os.Exit(0)
	}
	// otherwise call Go's standard testing.Main
	os.Exit(m.Run())
}

func issueControlRequest(controlAction string, printErr bool) error {
	var (
		ctx     = context.Background()
		cmdline = []string{
			filepath.Base(os.Args[0]),
			service.Name, controlAction,
		}
		stderr       *os.File
		discard, err = os.OpenFile(os.DevNull, os.O_RDWR, 0o755)
	)
	if err != nil {
		return err
	}
	defer discard.Close()
	if printErr {
		stderr = os.Stderr
	} else {
		stderr = discard
	}

	return cli.Run(ctx, testRoot, cmdline,
		discard, discard, stderr,
		service.MakeEnvironment, service.MakeExecutor)
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
				printError    = !shouldError
			)
			t.Run(controlAction, func(t *testing.T) {
				err := issueControlRequest(controlAction, printError)
				if shouldError &&
					err == nil {
					t.Fatalf("control \"%s\" was supposed to return an error, but did not",
						controlAction)
				}
				if !shouldError &&
					err != nil {
					t.Fatalf("control \"%s\" returned error: %s",
						controlAction, err)
				}
			})
		}
		waitForUninstall(t)
	})
	t.Run("good sequence", func(t *testing.T) {
		const printError = true
		for _, testControl := range []string{
			"install",
			"start",
			"restart",
			"stop",
			"uninstall",
		} {
			controlAction := testControl
			t.Run(controlAction, func(t *testing.T) {
				if err := issueControlRequest(controlAction, printError); err != nil {
					t.Fatal(err)
				}
			})
		}
		waitForUninstall(t)
	})
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
		const printError = true
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
				err := issueControlRequest(controlAction, printError)
				if err != nil {
					t.Fatal(err)
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
		const printError = true
		if err := issueControlRequest("uninstall", printError); err != nil {
			t.Error(err)
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

	type cmdsResponse struct {
		value interface{}
		error
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
