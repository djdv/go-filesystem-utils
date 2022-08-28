package daemon

import (
	"io"
	"os/exec"
)

type stdio struct {
	in       io.WriteCloser
	out, err io.ReadCloser
}

func (sio stdio) Read(p []byte) (n int, err error) {
	return sio.out.Read(p)
}

func (sio stdio) Write(p []byte) (n int, err error) {
	return sio.in.Write(p)
}

func (sio stdio) Close() error {
	for _, c := range []io.Closer{sio.in, sio.out, sio.err} {
		if c != nil {
			if err := c.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func setupStdioIPC(cmd *exec.Cmd) (sio stdio, err error) {
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
