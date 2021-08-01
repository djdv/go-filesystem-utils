package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ipfs/go-ipfs/filesystem/manager/errors"
	"github.com/multiformats/go-multiaddr"
)

type (
	// Request is a Multiaddr formatted message, containing file system relevant values
	// (such as a file system API, target, etc.)
	Request multiaddr.Multiaddr
	// Requests is simply a series of requests.
	Requests = <-chan Request

	// Response contains the request that initiated it,
	// along with an error (if encountered).
	Response struct {
		Request
		Error error
		io.Closer
	}
	// Responses is simply a series of responses.
	Responses = <-chan Response
)

type (
	// Interface accepts bind `Request`s,
	// and typically stores relevant `Response`s within its `Index`.
	Interface interface {
		Binder
		Index
	}

	// Binder takes in a series of requests,
	// and returns a series of responses.
	// Responses should contain the request that initiated it,
	// along with either its closer, or an error.
	Binder interface {
		Bind(context.Context, Requests) Responses
	}

	// Index maintains a `List` of Responses.
	// Typically corresponding to a range of responses from `Bind`.
	Index interface {
		List(context.Context) Responses
	}
)

// ParseArguments inspects the input strings and transforms them into a series of typed `Request`s if possible.
// Closing the output streams on cancel or an encountered error.
func ParseArguments(ctx context.Context, arguments ...string) (Requests, errors.Stream) {
	requests, errors := make(chan Request, len(arguments)), make(chan error)
	go func() {
		defer close(requests)
		defer close(errors)
		for _, maddrString := range arguments {
			ma, err := multiaddr.NewMultiaddr(maddrString)
			if err != nil {
				err = fmt.Errorf("failed to parse system arguments: %w", err)
				select {
				case errors <- err:
				case <-ctx.Done():
				}
				return
			}

			select {
			case requests <- ma:
			case <-ctx.Done():
				return
			}
		}
	}()
	return requests, errors
}

type encodableResponse struct {
	Request []byte `json:"request"`
	Error   string `json:"error,omitempty" xml:",omitempty"`
}

// TODO: needs text encoder for error values e.g. `--enc=textnl` shows request only
// output should be `[request\n`|`request\terrorstring\n]`
func (response Response) MarshalJSON() ([]byte, error) {
	if response.Request == nil {
		return nil, fmt.Errorf("response's Request field must be populated")
	}

	encoded := encodableResponse{Request: response.Bytes()}
	if response.Error != nil {
		encoded.Error = response.Error.Error()
	}

	return json.Marshal(encoded)
}

func (resp *Response) UnmarshalJSON(b []byte) (err error) {
	if len(b) < 2 || bytes.Equal(b, []byte("{}")) {
		err = fmt.Errorf("response was empty or short")
		return
	}

	decoded := new(encodableResponse)
	json.Unmarshal(b, decoded)

	if decoded.Error != "" {
		switch decoded.Error {
		case errors.Unwound.Error():
			resp.Error = errors.Unwound
		default:
			resp.Error = fmt.Errorf(decoded.Error)
		}
	}
	resp.Request, err = multiaddr.NewMultiaddrBytes(decoded.Request)
	return
}
