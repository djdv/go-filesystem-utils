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
		ListenerMaddr *formats.Multiaddr `json:",omitempty"`
		Info          string             `json:",omitempty"`
		Status        Status             `json:",omitempty"`
		StopReason    StopReason         `json:",omitempty"`
	}
	responsePair struct {
		*Response
		error
	}
	ResponseCallback func(*Response) error
	Environment      interface {
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
		if encodedMaddr := response.ListenerMaddr; encodedMaddr != nil {
			return fmt.Sprintf("Listening on: %s", encodedMaddr.Interface)
		}
		return ipc.SystemServiceDisplayName + " is starting..."
	case Ready:
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

/*
TODO: Check English.
TODO: this function changed dramatically, rework this into the new form.

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
// TODO: Check English.
// SplitResponse splits the processing of the cmds Response object into 2 phases:
// initialization and post-initialization.
//
// Response data and the protocol sequence will be validated internally,
// allowing the caller to ignore the details of the protocol.
// Callback functions are optional and may be nil.
//
// The returned startup function will block until the daemon reports that it's
// ready for operations.
//
// The returned runtime function will block until the daemon stops running.
func SplitResponse(ctx context.Context, response cmds.Response,
	startupCb, runtimeCb ResponseCallback) (startupFn, runtimeFn func() error) {
	var (
		startupResponses = make(chan responsePair)
		runtimeResponses = make(chan responsePair)
	)
	startupFn = makeStartupFunc(startupCb, startupResponses)
	runtimeFn = makeRuntimeFunc(runtimeCb, runtimeResponses)

	go dispatchResponses(ctx, response, startupResponses, runtimeResponses)

	return
}

// dispatchResponses reads the cmds.Response and
// sends our own Response type to the input channels.
// Dispatching to the startup channel during daemon startup,
// with the remainder going to the runtime channel afterwards.
func dispatchResponses(ctx context.Context, response cmds.Response,
	startup, runtime chan<- responsePair) {
	defer func() {
		// If we return early for whatever reason -
		// make sure the output channels get closed.
		for _, ch := range [...]chan<- responsePair{runtime, startup} {
			if ch != nil {
				close(ch)
			}
		}
	}()

	responses := (chan<- responsePair)(startup)
	for {
		untypedResponse, err := response.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			select {
			case responses <- responsePair{error: err}:
			case <-ctx.Done():
			}
			return
		}

		typedResponse, isResponse := untypedResponse.(*Response)
		if !isResponse {
			err := cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type: %#v", untypedResponse,
			)
			select {
			case responses <- responsePair{error: err}:
			case <-ctx.Done():
			}
			return
		}

		select {
		case responses <- responsePair{Response: typedResponse}:
		case <-ctx.Done():
			return
		}

		// Startup is finished, stop sending to its channel
		// and start directing responses to the runtime channel.
		if typedResponse.Status == Ready {
			close(startup)
			startup = nil
			responses = (chan<- responsePair)(runtime)
		}
	}
}

// makeStartupFunc validates the startup sequence
// while relaying valid responses to the provided callback.
func makeStartupFunc(callback ResponseCallback, responses <-chan responsePair) func() error {
	var sawHeader, startupFinished bool
	return func() error {
		if startupFinished {
			return errors.New("response is already beyond the startup sequence")
		}

		for response := range responses {
			if err := response.error; err != nil {
				return err
			}
			response := response.Response

			switch response.Status {
			case Starting:
				if response.ListenerMaddr == nil {
					if sawHeader {
						return fmt.Errorf("%w - received \"starting\" twice",
							ErrStartupSequence)
					}
					sawHeader = true
				}
			case Ready:
				if !sawHeader {
					return fmt.Errorf("%w - got response before \"starting\": %#v",
						ErrStartupSequence, response)
				}
				if startupFinished {
					return fmt.Errorf("%w - received \"ready\" twice",
						ErrStartupSequence)
				}
				startupFinished = true
			default:
				return fmt.Errorf("%w - got unexpected response during startup: %#v",
					ErrStartupSequence, response)
			}

			if callback != nil {
				if err := callback(response); err != nil {
					return err
				}
			}
		}

		if !startupFinished {
			return fmt.Errorf("%w: response closed before startup sequence finished",
				ErrStartupSequence)
		}

		return nil
	}
}

// makeRuntimeFunc does loose validation of responses
// and relays them to the provided callback.
func makeRuntimeFunc(callback ResponseCallback, responses <-chan responsePair) func() error {
	var runtimeFinished bool
	return func() error {
		if runtimeFinished {
			return errors.New("response is already beyond the runtime sequence")
		}

		for response := range responses {
			if err := response.error; err != nil {
				return err
			}
			response := response.Response

			switch response.Status {
			case Starting, Ready:
				return fmt.Errorf("%w - got unexpected response during runtime: %#v",
					ErrStartupSequence, response)
			default:
				if callback != nil {
					if err := callback(response); err != nil {
						return err
					}
				}
			}
		}

		runtimeFinished = true
		return nil
	}
}
