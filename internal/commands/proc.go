package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
)

type (
	cmdIO struct {
		in       io.WriteCloser
		out      io.ReadCloser
		closeErr error
		once     sync.Once
	}
	cmdIPCSignal = byte
)

const (
	EOT                         = 0x4
	ipcProcRelease cmdIPCSignal = EOT
	// stdio Clients should signal this file
	// immediately before closing, so the subprocess
	// can be aware it is about to be decoupled.
	ipcReleaseFileName = "release"
)

func (cio *cmdIO) Read(p []byte) (n int, err error) {
	return cio.out.Read(p)
}

func (cio *cmdIO) Write(p []byte) (n int, err error) {
	return cio.in.Write(p)
}

func (cio *cmdIO) Close() (err error) {
	cio.once.Do(func() {
		var errs []error
		for _, c := range []io.Closer{cio.in, cio.out} {
			if cErr := c.Close(); cErr != nil {
				errs = append(errs, cErr)
			}
		}
		if errs != nil {
			cio.closeErr = errors.Join(errs...)
		}
	})
	return cio.closeErr
}

func spawnDaemonProc(exitInterval time.Duration) (*exec.Cmd, *cmdIO, io.ReadCloser, error) {
	cmd, err := newDaemonCommand(exitInterval)
	if err != nil {
		return nil, nil, nil, err
	}
	cmd.SysProcAttr = emancipatedSubproc()
	cmdIO, stderr, err := setupCmdIPC(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, errors.Join(err, cmdIO.Close(), stderr.Close())
	}
	return cmd, cmdIO, stderr, nil
}

func newDaemonCommand(exitInterval time.Duration) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	const (
		mandatoryArgs = 1
		likelyArgs    = 2
	)
	args := make([]string, mandatoryArgs, likelyArgs)
	args[0] = daemonCommandName
	if exitInterval > 0 {
		args = append(args,
			fmt.Sprintf(
				"-%s=%s",
				exitAfterFlagName, exitInterval,
			),
		)
	}
	return exec.Command(self, args...), nil
}

func setupCmdIPC(cmd *exec.Cmd) (*cmdIO, io.ReadCloser, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, errors.Join(err, stdin.Close())
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, errors.Join(err, stdin.Close(), stdout.Close())
	}
	return &cmdIO{in: stdin, out: stdout}, stderr, nil
}

func getListenersFromProc(ipc io.ReadWriteCloser, stderr io.ReadCloser, options ...p9.ClientOpt) ([]multiaddr.Multiaddr, error) {
	var (
		stdErrs     = watchStderr(stderr)
		client, err = newClient(ipc, options...)
		errs        []error
	)
	if err != nil {
		errs = append(errs, fmt.Errorf(
			"could not connect to daemon: %w", err,
		))
		// An errant process should close stderr,
		// but we won't trust it.
		const exitGrace = 512 * time.Millisecond
		select {
		case err := <-stdErrs:
			if err != nil {
				errs = append(errs, err)
			}
		case <-time.After(exitGrace): // Rogue process.
		}
		return nil, errors.Join(errs...)
	}
	var (
		done           = make(chan struct{})
		maddrs         []multiaddr.Multiaddr
		fetchListeners = func() {
			defer close(done)
			var err error
			if maddrs, err = client.getListeners(); err != nil {
				errs = append(errs, fmt.Errorf(
					"subproccess protocol error: %w", err,
				))
			}
		}
		accumulateErr = func(err error) {
			if err != nil {
				errs = append(errs, err)
			}
		}
	)
	go fetchListeners()
	select {
	case <-done:
		if errs != nil {
			accumulateErr(client.Shutdown(patientShutdown))
		} else if len(maddrs) == 0 {
			errs = append(errs, fmt.Errorf(
				"%w: daemon didn't return any addresses",
				errServiceNotFound,
			))
		}
		accumulateErr(client.ipcRelease())
		accumulateErr(stderr.Close())
	case err := <-stdErrs:
		errs = append(errs, err, stderr.Close())
	}
	accumulateErr(client.Close())
	if errs != nil {
		return nil, errors.Join(errs...)
	}
	return maddrs, nil
}

func watchStderr(stderr io.Reader) <-chan error {
	errs := make(chan error, 1)
	go func() {
		defer close(errs)
		stdErr, err := io.ReadAll(stderr)
		if err != nil {
			errs <- err
			return
		}
		if len(stdErr) != 0 {
			errs <- fmt.Errorf(
				"subprocess stderr:"+
					"\n%s",
				stdErr,
			)
		}
	}()
	return errs
}

func (c *Client) ipcRelease() error {
	controlDir, err := (*p9.Client)(c).Attach(controlFileName)
	if err != nil {
		return err
	}
	_, releaseFile, err := controlDir.Walk([]string{ipcReleaseFileName})
	if err != nil {
		err = receiveError(controlDir, err)
		return errors.Join(err, controlDir.Close())
	}
	if _, _, err := releaseFile.Open(p9.WriteOnly); err != nil {
		err = receiveError(controlDir, err)
		return errors.Join(err, releaseFile.Close(), controlDir.Close())
	}
	data := []byte{ipcProcRelease}
	if _, err := releaseFile.WriteAt(data, 0); err != nil {
		err = receiveError(controlDir, err)
		return errors.Join(err, releaseFile.Close(), controlDir.Close())
	}
	return errors.Join(releaseFile.Close(), controlDir.Close())
}

func maybeKill(cmd *exec.Cmd) error {
	proc := cmd.Process
	if proc == nil {
		return nil
	}
	if !procRunning(proc) {
		return nil
	}
	if err := proc.Kill(); err != nil {
		var (
			pid  = proc.Pid
			name = filepath.Base(cmd.Path)
		)
		return fmt.Errorf("could not terminate subprocess (ID: %d; %s): %w",
			pid, name, err)
	}
	return nil
}
