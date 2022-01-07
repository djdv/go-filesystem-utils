package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	fscmds "github.com/djdv/go-filesystem-utils/cmd/filesystem"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	Status   uint
	Response struct {
		ListenerMaddr *fscmds.Multiaddr  `json:",omitempty"`
		Info          string             `json:",omitempty"`
		Status        Status             `json:",omitempty"`
		StopReason    environment.Reason `json:",omitempty"`
	}
	ResponseResult struct {
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

func (status Status) String() string {
	switch status {
	case Starting:
		return "starting"
	case Ready:
		return "ready"
	case Stopping:
		return "stopping"
	default:
		panic(fmt.Errorf("unexpected Status format: %#v", status))
	}
}

func (response *Response) String() string {
	switch status := response.Status; status {
	case Starting:
		if encodedMaddr := response.ListenerMaddr; encodedMaddr != nil {
			return fmt.Sprintf("listening on: %s", encodedMaddr.Interface)
		}
		return status.String()
	case Ready:
		return status.String()
	case Stopping:
		return fmt.Sprintf(
			"%s: %s",
			status.String(), response.StopReason.String(),
		)
	default:
		if response.Info != "" {
			return response.Info
		}
		panic(fmt.Errorf("unexpected response format: %#v",
			response,
		))
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
// TODO: deprecate / remove
func SplitResponse(response cmds.Response,
	startupCb, runtimeCb ResponseCallback) (startupFn, runtimeFn func() error) {
	results := responsesFromCmds(response)
	startupFn = func() error {
		if err := UntilStarting(results, nil); err != nil {
			return err
		}
		return UntilReady(results, startupCb)
	}
	runtimeFn = func() error { return UntilFinished(results, runtimeCb) }
	return
}

func UntilStarting(results <-chan ResponseResult, callback ResponseCallback) error {
	for result := range results {
		if err := result.error; err != nil {
			return err
		}
		if result.Status == Starting {
			if result.ListenerMaddr != nil {
				return fmt.Errorf("expected ListenerMaddr to be nil on process starting")
			}
			return nil
		}
		if callback != nil {
			if err := callback(result.Response); err != nil {
				return err
			}
		}
	}
	return nil
}

func UntilReady(results <-chan ResponseResult, callback ResponseCallback) error {
	for result := range results {
		if err := result.error; err != nil {
			return err
		}
		switch result.Status {
		case Ready:
			return nil
		case Starting:
			maddr := result.ListenerMaddr
			if maddr == nil {
				return fmt.Errorf(
					"expected ListenerMaddr to be populated"+
						"\n\tgot:%#v", result.Response)
			}
		}
		if callback != nil {
			if err := callback(result.Response); err != nil {
				return err
			}
		}
	}
	return nil
}

func UntilFinished(results <-chan ResponseResult, callback ResponseCallback) error {
	for result := range results {
		if err := result.error; err != nil {
			return err
		}
		if callback != nil {
			if err := callback(result.Response); err != nil {
				return err
			}
		}
	}
	return nil
}

// TODO: export?
func responsesFromCmds(cmdsResponse cmds.Response) <-chan ResponseResult {
	results := make(chan ResponseResult, cmdsResponse.Length())
	go func() {
		defer close(results)
		for {
			untypedResponse, err := cmdsResponse.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				results <- ResponseResult{error: err}
				return
			}
			response, err := assertResponse(untypedResponse)
			results <- ResponseResult{
				Response: response,
				error:    err,
			}
		}
	}()
	return results
}

func assertResponse(untypedResponse interface{}) (*Response, error) {
	typedResponse, isResponseType := untypedResponse.(*Response)
	if !isResponseType {
		return nil, cmds.Errorf(cmds.ErrImplementation,
			"cmds emitter sent unexpected type: %#v", untypedResponse,
		)
	}
	return typedResponse, nil
}

func ResponsesFromReaders(ctx context.Context, input, errInput io.Reader) <-chan ResponseResult {
	return spliceResultsAndErrs(
		responsesFromReader(ctx, input),
		errorsFromReader(ctx, errInput),
	)
}

func spliceResultsAndErrs(responses <-chan ResponseResult, errs <-chan error) <-chan ResponseResult {
	results := make(chan ResponseResult)
	go func() {
		defer close(results)
		for responses != nil ||
			errs != nil {
			select {
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				results <- ResponseResult{error: err}
			case result, ok := <-responses:
				if !ok {
					responses = nil
					continue
				}
				results <- result
			}
		}
	}()
	return results
}

func responsesFromReader(ctx context.Context,
	stdout io.Reader) <-chan ResponseResult {
	results := make(chan ResponseResult)
	go func() {
		defer close(results)
		serviceDecoder := json.NewDecoder(stdout)
		for {
			serviceResponse := new(Response)
			if err := serviceDecoder.Decode(serviceResponse); err != nil {
				if !errors.Is(err, io.EOF) {
					select {
					case results <- ResponseResult{error: err}:
					case <-ctx.Done():
					}
				}
				return
			}
			select {
			case results <- ResponseResult{Response: serviceResponse}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return validateResponses(results)
}

func validateResponses(responses <-chan ResponseResult) <-chan ResponseResult {
	results := make(chan ResponseResult, cap(responses))
	go func() {
		defer close(results)
		firstResponse := true
		for result := range responses {
			if err := result.error; err != nil {
				results <- result
				continue
			}
			if err := validateResponse(firstResponse, result.Response); err != nil {
				results <- ResponseResult{error: err}
				continue
			}
			firstResponse = false
			results <- result
		}
	}()
	return results
}

func validateResponse(firstResponse bool, response *Response) (err error) {
	switch response.Status {
	case Starting:
		encodedMaddr := response.ListenerMaddr
		if firstResponse &&
			encodedMaddr != nil {
			err = fmt.Errorf("did not expect maddr with starting response: %v", encodedMaddr)
		}
		if !firstResponse &&
			encodedMaddr == nil {
			err = errors.New("listener response did not contain listener maddr")
		}
	case Status(0):
		if response.Info == "" {
			err = errors.New("malformed message / empty values")
		}
	case Ready:
	default:
		err = fmt.Errorf("unexpected message: %#v", response)
	}
	return
}

func errorsFromReader(ctx context.Context, errorReader io.Reader) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		scanner := bufio.NewScanner(errorReader)
		for {
			if !scanner.Scan() {
				return
			}
			text := scanner.Text()
			select {
			case errs <- fmt.Errorf("got error from stderr: %s", text):
			case <-ctx.Done():
				return
			}
		}
	}()
	return errs
}
