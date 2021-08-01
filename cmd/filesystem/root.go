package fscmds

import (
	"context"

	"github.com/djdv/go-filesystem-utils/filesystem/manager"
	"github.com/djdv/go-filesystem-utils/filesystem/manager/errors"
)

const (
	serviceTarget = "filesystem.service"

	//ipcDaemonOptionKwd         = "daemon"
	//ipcDaemonOptionDescription = "TODO: daemon help text; it waits in the background and maintains connections to the IPFS node."

	rootServiceOptionKwd         = "api"
	rootServiceOptionDescription = "File system service multiaddr to use."

	rootIPFSOptionKwd         = "ipfs"
	rootIPFSOptionDescription = "IPFS API multiaddr to use."
)

func emitResponses(ctx context.Context, emit cmdsEmitFunc, requestErrors errors.Stream, responses manager.Responses) (allErrs []error) {
	var emitErr error
	for responses != nil || requestErrors != nil {
		select {
		case response, ok := <-responses:
			if !ok {
				responses = nil
				continue
			}
			if emitErr = emit(response); emitErr != nil {
				allErrs = append(allErrs, emitErr)            // emitter encountered a fault
				emit = func(interface{}) error { return nil } // stop emitting values to its observer
			}
			if response.Error != nil {
				allErrs = append(allErrs, response.Error)
			}

		case err, ok := <-requestErrors:
			if !ok {
				requestErrors = nil
				continue
			}
			allErrs = append(allErrs, err)

		case <-ctx.Done():
			return
		}
	}
	return
}
