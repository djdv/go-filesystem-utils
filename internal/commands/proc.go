package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
)

type (
	cmdIO struct {
		in       io.WriteCloser
		out, err io.ReadCloser
	}
	listenerResult struct {
		error
		maddrs []multiaddr.Multiaddr
	}
)

func (cio cmdIO) Read(p []byte) (n int, err error) {
	return cio.out.Read(p)
}

func (cio cmdIO) Write(p []byte) (n int, err error) {
	return cio.in.Write(p)
}

func (cio cmdIO) Close() (err error) {
	for _, c := range []io.Closer{cio.in, cio.out, cio.err} {
		if cErr := c.Close(); cErr != nil {
			err = fserrors.Join(err, cErr)
		}
	}
	return err
}

func setupCmdIPC(cmd *exec.Cmd) (sio cmdIO, err error) {
	if sio.in, err = cmd.StdinPipe(); err != nil {
		return
	}
	if sio.out, err = cmd.StdoutPipe(); err != nil {
		return
	}
	if sio.err, err = cmd.StderrPipe(); err != nil {
		return
	}
	return
}

func selfCommand(args []string, exitInterval time.Duration) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, args...)
	if exitInterval != 0 {
		cmd.Args = append(cmd.Args,
			fmt.Sprintf("-exit-after=%s", exitInterval),
		)
	}
	return cmd, nil
}

func getListenersFrom(cio cmdIO, fsysName string) ([]multiaddr.Multiaddr, error) {
	var (
		stderrs       = watchStderr(cio.err)
		clientResults = getListenersAsync(cio, fsysName)
	)
	for {
		select {
		case err, ok := <-stderrs:
			if ok {
				return nil, err
			}
			stderrs = nil
		case result := <-clientResults:
			return result.maddrs, result.error
		}
	}
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
			errs <- fmt.Errorf("stderr: %s", stdErr)
		}
	}()
	return errs
}

func getListenersAsync(cio io.ReadWriteCloser, fsysName string) <-chan listenerResult {
	results := make(chan listenerResult, 1)
	go func() {
		defer close(results)
		client, err := p9.NewClient(cio)
		if err != nil {
			results <- listenerResult{
				error: fmt.Errorf("could not create client: %w", err),
			}
			return
		}
		listenersDir, err := client.Attach(fsysName)
		if err != nil {
			results <- listenerResult{
				error: fmt.Errorf(`could not attach to "%s": %w`, fsysName, err),
			}
			return
		}
		maddrs, err := p9fs.GetListeners(listenersDir)
		if err = fserrors.Join(err, listenersDir.Close()); err != nil {
			results <- listenerResult{
				error: fmt.Errorf(`could not parse listeners from "%s": %w`, fsysName, err),
			}
			return
		}
		results <- listenerResult{maddrs: maddrs}
	}()
	return results
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
