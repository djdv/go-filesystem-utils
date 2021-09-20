package service

import (
	"errors"
	"fmt"
	"io"

	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// TODO: not this
// Either make a custom emitter that wraps chanemitter
// or just emit to the syslog in the handler funcs
// ^ this seems more sensible; construct the syslog on run and attach it to the service interface
// ^^ or just use the one passed to us on Start()
func formatSystemService(response cmds.Response, emitter cmds.ResponseEmitter) error {
	var (
		request         = response.Request()
		ctx             = request.Context
		settings        = new(Settings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
	)
	if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
		return err
	}

	outputs := formats.MakeOptionalOutputs(response.Request(), emitter)

	for {
		untypedResponse, err := response.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			return nil
		}

		response, ok := untypedResponse.(*ipc.ServiceResponse)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type+value: %#v", untypedResponse)
		}

		switch response.Status {
		case ipc.ServiceStarting:
			outputs.Print(ipc.StdHeader + "\n")
		case ipc.ServiceReady:
			if encodedMaddr := response.ListenerMaddr; encodedMaddr != nil {
				outputs.Print(fmt.Sprintf("%s%s\n", ipc.StdGoodStatus, encodedMaddr.Interface))
			} else {
				outputs.Print(ipc.StdReady + "\n")
				outputs.Print("Send interrupt to stop\n")
			}
		case ipc.ServiceError:
			if errMsg := response.Info; errMsg == "" {
				outputs.Error(errors.New("service responded with an error status, but no message\n"))
			} else {
				outputs.Error(errors.New(errMsg + "\n"))
			}
		default:
			if response.Info != "" {
				outputs.Print(response.Info + "\n")
			}
		}

		outputs.Emit(response)
	}
}
