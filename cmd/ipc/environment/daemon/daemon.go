package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
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
		InitializeStop(context.Context) (<-chan StopReason, error)
		Stop(StopReason) error
	}
	daemonEnvironment struct {
		stopCtx  context.Context
		stopChan chan StopReason
	}
)

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
)

func (response *Response) String() string {
	switch response.Status {
	case Starting:
		return ipc.SystemServiceDisplayName + " is starting..."
	case Ready:
		if encodedMaddr := response.ListenerMaddr; encodedMaddr != nil {
			return fmt.Sprintf("Listening on: %s", encodedMaddr.Interface)
		}
		return ipc.SystemServiceDisplayName + " is ready"
	case Stopping:
		return fmt.Sprintf("Stopping: %s", response.StopReason.String())
	default:
		if response.Info == "" {
			panic(fmt.Errorf("unexpected response format: %#v",
				response,
			))
		}
		return response.Info
	}
}

var ErrStartupSequence = errors.New("startup sequence was not in order")

func NewDaemonEnvironment() Environment { return &daemonEnvironment{} }

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

func makeStartupChecker() func(response *Response) (bool, error) {
	var sawHeader bool
	return func(response *Response) (startupFinished bool, err error) {
		switch response.Status {
		case Starting:
			if response.ListenerMaddr == nil {
				if sawHeader {
					err = fmt.Errorf("%w - received \"starting\" twice",
						ErrStartupSequence)
					return
				}
				sawHeader = true
			}
			return
		case Ready:
			if !sawHeader {
				err = fmt.Errorf("%w - got response before \"starting\": %#v",
					ErrStartupSequence, response)
				return
			}
			if startupFinished {
				err = fmt.Errorf("%w - received \"ready\" twice",
					ErrStartupSequence)
				return
			}
			startupFinished = true
			return
		default:
			err = fmt.Errorf("%w - got unexpected response during startup: %#v",
				ErrStartupSequence, response)
			return
		}
	}
}

/*
TODO: Check English.

ParseResponse handles responses from the server.
Validating the response data as well as the startup sequence documented below.
If a sequence error is encountered `ErrResponseSequence` will be returned on the channel.
Otherwise responses will be sent to their channel as they're received.

A standard implementation of the service daemon is expected to respond
with the following sequence of responses.

 // Required - Empty "starting" response:
 Response{Status: Starting}

 // Optional - List of listeners
  Response{
  	Status: Starting,
  	ListenerMaddr: non-nil-maddr,
  }

 // Required - Empty "Ready" response:
  Response{Status: Ready}

I.e.
 Sequence_start     = Starting;
 Sequence_listeners = Starting, Listener;
 Sequence_end       = Ready;
 Sequence           = Sequence_start, {Sequence_listeners}, Sequence_end;
*/
func ParseResponse(ctx context.Context, response cmds.Response) (<-chan Response, <-chan error) {
	var (
		responses = make(chan Response)
		errs      = make(chan error)
	)
	go func() {
		defer close(responses)
		defer close(errs)
		var (
			checkStartupSequence = makeStartupChecker()
			finishedStarting     bool
		)
		for {
			untypedResponse, err := response.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					if finishedStarting {
						return
					}
					err = fmt.Errorf("%w: response closed before startup sequence finished",
						ErrStartupSequence)
				}
				select {
				case errs <- err:
				case <-ctx.Done():
				}
				return
			}

			response, ok := untypedResponse.(*Response)
			if !ok {
				select {
				case errs <- cmds.Errorf(cmds.ErrImplementation,
					"emitter sent unexpected type: %#v", untypedResponse):
				case <-ctx.Done():
				}
				return
			}

			if !finishedStarting {
				var err error
				if finishedStarting, err = checkStartupSequence(response); err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
					}
					return
				}
			}
			select {
			case responses <- *response:
			case <-ctx.Done():
				return
			}
		}
	}()
	return responses, errs
}
