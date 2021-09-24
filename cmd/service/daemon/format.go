package daemon

import (
	"errors"
	"fmt"
	"io"

	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func formatDaemon(response cmds.Response, emitter cmds.ResponseEmitter) error {
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

		response, ok := untypedResponse.(*daemon.Response)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type+value: %#v", untypedResponse)
		}

		switch response.Status {
		case daemon.Starting:
			outputs.Print(ipc.StdoutHeader + "\n")
		case daemon.Ready:
			if encodedMaddr := response.ListenerMaddr; encodedMaddr != nil {
				outputs.Print(fmt.Sprintf("%s %s\n", ipc.StdoutListenerPrefix, encodedMaddr.Interface))
			} else {
				outputs.Print(ipc.StdServerReady + "\n")
				outputs.Print("Send interrupt to stop\n")
			}
		case daemon.Stopping:
			outputs.Print("Stopping: " + response.StopReason.String() + "\n")
		case daemon.Error:
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
