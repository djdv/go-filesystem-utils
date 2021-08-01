//go:build windows || plan9 || netbsd || openbsd
// +build windows plan9 netbsd openbsd

package bazil

import (
	"context"
	"fmt"

	"github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/filesystem"
	"github.com/ipfs/go-ipfs/filesystem/manager"
)

//func NewBinder(context.Context, filesystem.ID, *core.IpfsNode, bool) (manager.Binder, error) {
func NewBinder(_ context.Context, _ filesystem.ID, _ *core.IpfsNode, _ bool) (manager.Binder, error) {
	return new(unsupportedBinder), nil
}

type unsupportedBinder struct{}

func (*unsupportedBinder) Bind(ctx context.Context, requests manager.Requests) manager.Responses {
	responses := make(chan manager.Response, len(requests))
	go func() {
		defer close(responses)
		for request := range requests {
			select {
			case responses <- manager.Response{
				Request: request,
				Error:   fmt.Errorf("Bazil Fuse not supported in this build"),
			}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return responses
}
