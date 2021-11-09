package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"

	stopenv "github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	Status   uint
	Response struct {
		ListenerMaddr *formats.Multiaddr `json:",omitempty"`
		Info          string             `json:",omitempty"`
		Status        Status             `json:",omitempty"`
		StopReason    stopenv.Reason     `json:",omitempty"`
	}
	responsePair struct {
		*Response
		error
	}
	ResponseCallback func(*Response) error
)

const (
	_ Status = iota
	Starting
	Ready
	Stopping
)

var ErrStartupSequence = errors.New("startup sequence was not in order")

func (response *Response) String() string {
	switch response.Status {
	case Starting:
		if encodedMaddr := response.ListenerMaddr; encodedMaddr != nil {
			return fmt.Sprintf("listening on: %s", encodedMaddr.Interface)
		}
		return "starting..."
	case Ready:
		return "ready"
	case Stopping:
		return fmt.Sprintf("stopping: %s", response.StopReason.String())
	default:
		if response.Info == "" {
			panic(fmt.Errorf("unexpected response format: %#v",
				response,
			))
		}
		return response.Info
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
	var sawStarting, sawReady bool
	return func() error {
		if sawReady {
			return errors.New("response is already beyond the startup sequence")
		}

		for response := range responses {
			var (
				err      = response.error
				response = response.Response
			)
			if err != nil {
				return err
			}
			isStarting, isReady, err := checkResponse(response, sawStarting, sawReady)
			if err != nil {
				return err
			}
			if isStarting {
				sawStarting = true
			}
			if isReady {
				sawReady = true
			}
			if callback != nil {
				if err := callback(response); err != nil {
					return err
				}
			}
		}
		if !sawReady {
			return fmt.Errorf("%w: response closed before startup sequence finished",
				ErrStartupSequence)
		}
		return nil
	}
}

func checkResponse(response *Response, sawStarting, sawReady bool) (starting, ready bool, err error) {
	switch response.Status {
	case Starting:
		if err = checkResponseStarting(response, sawStarting); err != nil {
			return
		}
		starting = true
	case Ready:
		if err = checkResponseReady(response, sawStarting, sawReady); err != nil {
			return
		}
		ready = true
	case Status(0):
		if response.Info == "" {
			err = fmt.Errorf("%w - got empty/malformed response during startup: %#v",
				ErrStartupSequence, response)
		}
	default:
		err = fmt.Errorf("%w - got unexpected response during startup: %#v",
			ErrStartupSequence, response)
	}
	return
}

func checkResponseStarting(response *Response, alreadySeen bool) error {
	if response.ListenerMaddr == nil {
		if alreadySeen {
			return fmt.Errorf("%w - received \"starting\" twice",
				ErrStartupSequence)
		}
	}
	return nil
}

func checkResponseReady(response *Response, sawStarting, sawReady bool) error {
	if !sawStarting {
		return fmt.Errorf("%w - got response before \"starting\": %#v",
			ErrStartupSequence, response)
	}
	if sawReady {
		return fmt.Errorf("%w - received \"ready\" twice",
			ErrStartupSequence)
	}
	return nil
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
