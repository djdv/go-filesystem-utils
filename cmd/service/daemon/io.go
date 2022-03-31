package daemon

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
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

func handleStderr(err error) error {
	stdStream := os.Stderr
	var (
		_, sErr       = stdStream.Stat()
		errStreamOpen = sErr == nil
		haveError     = err != nil
	)
	if !errStreamOpen {
		if !haveError {
			return reattachToDiscardIO(stdStream, fs.FileMode(os.O_RDWR))
		}
		var (
			streamName     = stdStream.Name()
			streamBaseName = filepath.Base(streamName)
			namePattern    = tempfilePatternName(streamBaseName)
		)
		if fErr := writeErrToTemporaryFile(namePattern, err); fErr != nil {
			err = maybeWrapErr(err, fErr)
			return err
		}
		if nullErr := reattachToDiscardIO(stdStream, fs.FileMode(os.O_RDWR)); nullErr != nil {
			err = maybeWrapErr(err, nullErr)
		}
	}
	return err
}

func maybeWrapErr(car, cdr error) error {
	if car == nil {
		return cdr
	} else if cdr != nil {
		return fmt.Errorf("%w\n\t%s", car, cdr)
	}
	return car
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

// TODO: rename? synchronizeWithEmitter
// TODO: document our protocol; uses ASCIIEOT as signal to close all stdio streams.
// E.g. emulated process "detach" signal.
func synchronizeWithStdio(ctx context.Context, emitter cmds.ResponseEmitter,
	// stdin, stdout, stderr *os.File) (chan<- *Response, errCh, error) {
	stdin, stdout, stderr *os.File,
) (cmds.ResponseEmitter, errCh, error) {
	if !isPipe(stdin) {
		errs := make(chan error)
		close(errs)
		return emitter, errs, nil
	}

	stdoutStat, err := stdout.Stat()
	if err != nil {
		return nil, nil, err
	}
	var (
		stdoutMode = stdoutStat.Mode()

		errs      = make(chan error)
		stdioMu   = new(sync.RWMutex)
		muEmitter = &mutexEmitter{
			ResponseEmitter: emitter,
			mu:              stdioMu.RLocker(),
		}
	)
	go func() {
		defer close(errs)
		for err := range signalFromReader(ctx, ASCIIEOT, stdin) {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
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
				select {
				case errs <- err:
				case <-ctx.Done():
				}
				return
			}
		}

		// Send emits to nowhere.
		if err := reattachToDiscardIO(stdout, stdoutMode); err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
			return

		}
		// Leave stderr closed.
		// Caller can inspect+reopen dynamically
		// if/when it encounters an error.
	}()

	// emitterChan, emitErrs := emitterChan(ctx, muEmitter)
	return muEmitter, errs, nil
}

func isPipe(file *os.File) bool {
	fStat, err := file.Stat()
	if err != nil {
		return false
	}
	return fStat.Mode().Type()&os.ModeNamedPipe != 0
}

func signalFromReader(ctx context.Context, signal stdioSignal, reader io.Reader) errCh {
	var (
		bytesChan, scanErrs = scanBytes(ctx, reader)
		errs                = make(chan error)
	)
	go func() {
		defer close(errs)
		expected := []byte{signal}
		for signal := range bytesChan {
			if !bytes.Equal(expected, signal) {
				err := fmt.Errorf(
					"unexpected response on stdin"+
						"\n\t wanted: %#v"+
						"\n\tgot: %#v",
					expected, signal,
				)
				select {
				case errs <- err:
				case <-ctx.Done():
				}
				return
			}
		}

		var (
			buf, rErr      = io.ReadAll(reader)
			inputStillOpen = rErr == nil && len(buf) > 0
		)
		if inputStillOpen {
			err := fmt.Errorf(
				"expected stdin to be closed after writing EOT(%X)",
				ASCIIEOT,
			)
			select {
			case errs <- err:
			case <-ctx.Done():
				return
			}
		}
	}()

	return CtxMerge(ctx, scanErrs, errs)
}

func scanBytes(ctx context.Context, source io.Reader) (<-chan []byte, errCh) {
	var (
		out  = make(chan []byte)
		errs = make(chan error)
	)
	go func() {
		defer close(out)
		defer close(errs)
		scanner := bufio.NewScanner(source)
		scanner.Split(bufio.ScanBytes)
		for scanner.Scan() {
			select {
			case out <- scanner.Bytes():
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, errs
}
