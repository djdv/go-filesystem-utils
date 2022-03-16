package daemon

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	cmds "github.com/ipfs/go-ipfs-cmds"
)

// End Of Transmission `♦` may be sent to stdin.
// Sender must close stdin after sending the signal.
// Receiver will close stdout and stderr.
const ASCIIEOT stdioSignal = 0x4

type stdioSignal = byte

func reattachToDiscardIO(stdio *os.File, mode os.FileMode) error {
	discard, err := os.OpenFile(os.DevNull, int(mode), mode.Perm())
	if err != nil {
		return err
	}
	*stdio = *discard
	return nil
}

func handleStderr(stream *os.File, err error) error {
	var (
		_, sErr       = stream.Stat()
		errStreamOpen = sErr == nil
		haveError     = err != nil
	)
	if !errStreamOpen {
		if !haveError {
			return reattachToDiscardIO(stream, fs.FileMode(os.O_RDWR))
		}
		var (
			streamName     = stream.Name()
			streamBaseName = filepath.Base(streamName)
			namePattern    = tempfilePatternName(streamBaseName)
		)
		if fErr := writeErrToTemporaryFile(namePattern, err); fErr != nil {
			err = maybeWrapErr(err, fErr)
			return err
		}
		if nullErr := reattachToDiscardIO(stream, fs.FileMode(os.O_RDWR)); nullErr != nil {
			err = maybeWrapErr(err, nullErr)
		}
	}
	return err
}

/*
func handleStderr(stream *os.File, err error) error {
	var (
		_, sErr    = stream.Stat()
		streamOpen = sErr == nil
	)
	if !streamOpen {
		if err == nil {
			return reattachToDiscardIO(stream, fs.FileMode(os.O_RDWR))
		}
		var (
			streamName     = stream.Name()
			streamBaseName = filepath.Base(streamName)
			namePattern    = tempfilePatternName(streamBaseName)
		)
		if fErr := writeErrToTemporaryFile(namePattern, err); fErr != nil {
			return fmt.Errorf("%w\n\t%s", err, fErr)
		}
	}
	return err
}
*/

func tempfilePatternName(fileName string) string {
	var (
		execName    = filepath.Base(os.Args[0])
		serviceName = strings.TrimSuffix(execName, filepath.Ext(execName))
	)
	if fileName == "" {
		panic("temporary file must be provided with a name")
	}
	return fmt.Sprintf("%s.%s._*.log", serviceName, fileName)
}

func writeErrToTemporaryFile(namePattern string, err error) error {
	tempFile, fErr := os.CreateTemp("", namePattern)
	if fErr != nil {
		return fErr
	}
	if _, wErr := tempFile.WriteString(err.Error()); wErr != nil {
		return wErr
	}
	return nil
}

type mutexEmitter struct {
	cmds.ResponseEmitter
	mu sync.Locker
}

func (me *mutexEmitter) Emit(value interface{}) error {
	me.mu.Lock()
	defer me.mu.Unlock()
	return me.ResponseEmitter.Emit(value)
}

// TODO: document our protocol, uses ASCIIEOT as signal to close all stdio streams.
// E.g. emulated process "detatch" signal.
func synchronizeWithStdio(emitter cmds.ResponseEmitter,
	stdin, stdout, stderr *os.File) (cmds.ResponseEmitter, taskErr) {
	if !isPipe(stdin) {
		return emitter, taskErr{}
	}

	stdoutStat, err := stdout.Stat()
	if err != nil {
		err = fmt.Errorf("DBG stdio 1 :%w", err)
		return nil, taskErr{foreground: err}
	}
	var (
		stdoutMode = stdoutStat.Mode()

		ioErrs    = make(chan error)
		stdioMu   = new(sync.RWMutex)
		muEmitter = &mutexEmitter{
			ResponseEmitter: emitter,
			mu:              stdioMu.RLocker(),
		}
	)
	go func() {
		defer close(ioErrs)
		gotErr := false // TODO: convert to wrapped stack error; send in fewer places/branches
		for err := range signalFromReader(ASCIIEOT, stdin) {
			gotErr = true
			ioErrs <- err
		}
		if gotErr {
			return
		}
		// NOTE:
		// Stdin signaled to us that it's closing its ends of stdio.
		// Block calls to `Emit`.
		stdioMu.Lock()
		defer stdioMu.Unlock()

		// Close our ends of stdio.
		for _, closer := range []io.Closer{stdin, stdout, stderr} {
			if err := closer.Close(); err != nil {
				ioErrs <- err
			}
		}

		// Send emits to nowhere.
		if err := reattachToDiscardIO(stdout, stdoutMode); err != nil {
			ioErrs <- err
		}
		// Leave stderr closed.
		// Caller can inspect+reopen dynamically
		// if/when it encounters an error.
	}()

	return muEmitter, taskErr{background: ioErrs}
}

func isPipe(file *os.File) bool {
	fStat, err := file.Stat()
	if err != nil {
		return false
	}
	return fStat.Mode().Type()&os.ModeNamedPipe != 0
}

func signalFromReader(signal stdioSignal, reader io.Reader) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		scanner := bufio.NewScanner(reader)
		scanner.Split(bufio.ScanBytes)
		var err error
		for scanner.Scan() {
			var (
				expected = []byte{signal}
				buffer   = scanner.Bytes()
			)
			if !bytes.Equal(expected, buffer) {
				err = fmt.Errorf(
					"unexpected response on stdin"+
						"\n\t wanted: %#v"+
						"\n\tgot: %#v",
					expected, buffer,
				)
				break
			}
			var (
				buf, rErr      = io.ReadAll(reader)
				inputStillOpen = rErr == nil && len(buf) > 0
			)
			if inputStillOpen {
				err = fmt.Errorf(
					"expected stdin to be closed after writing EOT(%X)",
					ASCIIEOT,
				)
				break
			}
		}
		if sErr := scanner.Err(); sErr != nil {
			err = maybeWrapErr(err, sErr)
		}
		if err != nil {
			errs <- err
		}
	}()
	return errs
}
