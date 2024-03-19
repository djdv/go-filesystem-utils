package daemon

import (
	"errors"
	"io"
	"sync"
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
