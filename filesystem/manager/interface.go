package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/djdv/go-filesystem-utils/filesystem/manager/errors"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

type (
	// Request is a Multiaddr formatted message,
	// containing sets of file system relevant values
	// (such as a host system API, fs api, target, etc.)
	Request  multiaddr.Multiaddr
	Requests = <-chan Request

	// Interface accepts bind `Request`s,
	// and typically stores relevant `Response`s within its `Index`.
	// TODO: Proper separation + name. Maybe this is okay but needs review.
	Interface interface {
		Binder
		Index
	}

	// Binder takes in a series of requests,
	// and returns a series of responses.
	// Responses should contain the request that initiated it,
	// along with either its closer, or an error.
	// TODO: Same remark as on `Response`, type overloading like this is C-tier garbo.
	Binder interface {
		Bind(context.Context, Requests) Responses
	}

	// Response contains the request that initiated it.
	// And optionally a closer (if relevant to the request).
	// TODO: ^This is a bad design.
	// We were trying to keep a single format for each command to avoid writing a million formatters.
	// But guess what, we're going to write a million formatters.
	Response struct {
		Request
		Error error
		io.Closer
	}
	Responses = <-chan Response
	Index     interface {
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

type (
	encodableResponse struct {
		Request []byte `json:"request"`
		Error   string `json:"error,omitempty" xml:",omitempty"`
	}

	responseTextEncoder struct {
		w                         io.Writer
		prefixUnit, terminateUnit bool
	}
)

var (
	ResponseEncoderMap = cmds.EncoderMap{
		cmds.XML:  cmds.Encoders[cmds.XML],
		cmds.JSON: cmds.Encoders[cmds.JSON],
		cmds.Text: func(*cmds.Request) func(io.Writer) cmds.Encoder {
			return func(w io.Writer) cmds.Encoder { return &responseTextEncoder{w: w} }
		},
		cmds.TextNewline: func(*cmds.Request) func(io.Writer) cmds.Encoder {
			return func(w io.Writer) cmds.Encoder {
				return &responseTextEncoder{w: w, terminateUnit: true}
			}
		},
	}
	errEmptyResponseRequest = fmt.Errorf("response's Request field must be populated")
)

func (e *responseTextEncoder) Encode(v interface{}) error {
	response, ok := v.(*Response)
	if !ok {
		return fmt.Errorf("expected type %T got type %T",
			response, v)
	}
	if response.Request == nil {
		return errEmptyResponseRequest
	}

	const unitSeparator = '\x1F'
	var (
		request         = response.Request.String()
		requestErr      = response.Error
		encodingErr     error
		prefix, postfix string
	)

	if e.prefixUnit {
		prefix = string(unitSeparator)
	}
	if e.terminateUnit {
		postfix = "\n"
	}

	if requestErr == nil {
		_, encodingErr = fmt.Fprintf(e.w, "%s%s%s", prefix, request, postfix)
	} else {
		requestErrString := requestErr.Error()
		_, encodingErr = fmt.Fprintf(e.w, "%s%s - %s%s",
			prefix, request, requestErrString, postfix)
	}
	if encodingErr != nil {
		return encodingErr
	}

	if !e.terminateUnit &&
		!e.prefixUnit {
		e.prefixUnit = true
	}
	return nil
}

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

func (response *Response) UnmarshalJSON(b []byte) (err error) {
	if len(b) < 2 || bytes.Equal(b, []byte("{}")) {
		err = fmt.Errorf("response was empty or short")
		return
	}

	decoded := new(encodableResponse)
	json.Unmarshal(b, decoded)

	if decoded.Error != "" {
		response.Error = fmt.Errorf(decoded.Error)
	}
	response.Request, err = multiaddr.NewMultiaddrBytes(decoded.Request)
	return
}
