package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/djdv/go-filesystem-utils/cmd/formats"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	Status     uint
	StopReason uint
	Response   struct {
		Status        Status             `json:",omitempty"`
		StopReason    StopReason         `json:",omitempty"`
		ListenerMaddr *formats.Multiaddr `json:",omitempty"`
		Info          string             `json:",omitempty"`
	}
	Environment interface {
		// TODO: Initalizer / Start() needs to return a channel of "reason"
		// So that when the root context is canceled, daemon.Run can catch it
		// "we were canceled, stop execution"
		// Stop can take in a reason to control the return value from daemon.Run
		// And we can replace the idle watcher with it
		// watchdog{ notBusy? self.Stop(reasonIdle) }
		// trap { select { httpError? ... , envStop? why?(reason) => maybeErrorValue } }
		// This also allows an external command to call env.Stop(requestedToStop)
		// which returns no error from daemon.Run
		//
		InitializeStop(context.Context) (<-chan StopReason, error)
		Stop(StopReason) error
	}
	daemonEnvironment struct {
		stopCtx  context.Context
		stopChan chan StopReason
	}
)

type ResponseHandlerFunc func(*Response) error

func HandleInitSequence(response cmds.Response, callback ResponseHandlerFunc) error {
	var (
		// TODO: better error messages
		sequenceErr = fmt.Errorf("init sequence was not in order") // TODO: pkg-level?
		sawHeader   bool
	)
	for {
		untypedResponse, err := response.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			return fmt.Errorf("%w: response ended before init sequence terminated", sequenceErr)
		}

		response, ok := untypedResponse.(*Response)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type: %#v", untypedResponse)
		}
		if !sawHeader {
			if response.Status != Starting {
				return fmt.Errorf("%w - got response before \"starting\": %#v",
					sequenceErr, response)
			}
			sawHeader = true
			if err := callback(response); err != nil {
				return err
			}
			continue
		}

		switch response.Status {
		case Ready:
			if err := callback(response); err != nil {
				return err
			}
			if response.ListenerMaddr == nil {
				return nil
			}
		case Error:
			if errMsg := response.Info; errMsg != "" {
				return errors.New(errMsg)
			}
			return errors.New("daemon responded with an error status, but no message\n")
		default:
			if err := callback(response); err != nil {
				return err
			}
			// TODO: stubbed for debugging sysservice
			//return fmt.Errorf("%w - got unexpected response type: %#v",
			//	sequenceErr, response)
		}
	}
}

func HandleRunningSequence(response cmds.Response, callback ResponseHandlerFunc) error {
	for {
		untypedResponse, err := response.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			return nil
		}

		response, ok := untypedResponse.(*Response)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type: %#v", untypedResponse)
		}
		if err := callback(response); err != nil {
			return err
		}
	}
}

func NewDaemonEnvironment() Environment { return &daemonEnvironment{} }

//go:generate stringer -type=StopReason -linecomment
const (
	_               StopReason = iota
	RequestCanceled            // Request was canceled
	Idle                       // Service was idle
	StopRequested              // Stop was requested
)

const (
	_ Status = iota
	Starting
	Ready
	Stopping
	Error
)

func (de *daemonEnvironment) InitializeStop(ctx context.Context) (<-chan StopReason, error) {
	if de.stopChan != nil {
		return nil, fmt.Errorf("environment already initialized") // TODO: error message
	}
	stopChan := make(chan StopReason)
	de.stopChan = stopChan
	de.stopCtx = ctx
	return stopChan, nil
}

func (de *daemonEnvironment) Stop(reason StopReason) error {
	var (
		ctx      = de.stopCtx
		stopChan = de.stopChan
	)
	if stopChan == nil {
		return fmt.Errorf("environment not initialized") // TODO: error message+value
	}
	de.stopChan = nil
	defer close(stopChan)
	select {
	case stopChan <- reason:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
