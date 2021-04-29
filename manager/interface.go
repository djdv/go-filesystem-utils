package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

type (
	// Request is a Multiaddr formatted message,
	// containing sets of file system relevant values
	// (such as a host system API, fs api, target, etc.)
	Request  multiaddr.Multiaddr
	Requests = <-chan Request

	// Response contains the request that initiated it.
	// And optionally a closer (if relevant to the request).
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
