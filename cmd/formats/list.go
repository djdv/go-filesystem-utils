package formats

import (
	"context"
	"errors"
	"io"

	"github.com/djdv/go-filesystem-utils/manager"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// TODO: try to eliminate formats package; formatter should be specified with command.
// E.g. move this to list pkg.
// CmdsResponseToManagerResponses splits the cmds.Response stream
// into a manager.Response and error stream.
func CmdsResponseToManagerResponses(ctx context.Context, response cmds.Response) (manager.Responses, <-chan error) {
	var (
		responses  = make(chan manager.Response)
		cmdsErrors = make(chan error, 1)
	)
	go func() {
		defer close(responses)
		defer close(cmdsErrors)
		for {
			untypedResponse, err := response.Next()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					cmdsErrors <- err
				}
				return
			}

			response, ok := untypedResponse.(*manager.Response)
			if !ok {
				cmdsErrors <- cmds.Errorf(cmds.ErrImplementation,
					"emitter sent unexpected type+value: %#v", untypedResponse)
				return
			}

			select {
			case responses <- *response:
			case <-ctx.Done():
				return
			}
		}
	}()
	return responses, cmdsErrors
}
