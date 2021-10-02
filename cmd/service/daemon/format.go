package daemon

import (
	"errors"
	"io"

	"github.com/djdv/go-filesystem-utils/cmd/formats"
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

		if err := outputs.Print(response.String() + "\n"); err != nil {
			return err
		}

		if response.Status == daemon.Ready &&
			response.ListenerMaddr == nil {
			if err := outputs.Print("Send interrupt to stop\n"); err != nil {
				return err
			}
		}

		if err := outputs.Emit(response); err != nil {
			return err
		}
	}
}
