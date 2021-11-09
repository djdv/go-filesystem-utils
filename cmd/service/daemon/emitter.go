package daemon

import (
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	stopenv "github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

var errEmit = errors.New("failed to emit")

func wrapEmitErr(err error) error {
	if err != nil {
		err = fmt.Errorf("%s: %w", errEmit, err)
	}
	return err
}

func emitStatusResponse(emitter cmds.ResponseEmitter,
	status Status, stopReason stopenv.Reason) error {
	return wrapEmitErr(
		emitter.Emit(
			&Response{
				Status:     status,
				StopReason: stopReason,
			}))
}

func emitStarting(emitter cmds.ResponseEmitter) error {
	return emitStatusResponse(emitter, Starting, 0)
}

func emitReady(emitter cmds.ResponseEmitter) error {
	return emitStatusResponse(emitter, Ready, 0)
}

func emitStopping(emitter cmds.ResponseEmitter, reason stopenv.Reason) error {
	return emitStatusResponse(emitter, Stopping, reason)
}

func emitMaddrListener(emitter cmds.ResponseEmitter, maddr multiaddr.Multiaddr) error {
	return wrapEmitErr(
		emitter.Emit(&Response{
			Status:        Starting,
			ListenerMaddr: &formats.Multiaddr{Interface: maddr},
		}))
}

func emitInfof(emitter cmds.ResponseEmitter, fmtStr string, v ...interface{}) error {
	return wrapEmitErr(
		emitter.Emit(
			&Response{
				Info: fmt.Sprintf(fmtStr, v...),
			}))
}

func emitListener(emitter cmds.ResponseEmitter, name string) error {
	return emitInfof(emitter, "listening on: %s", name)
}

func emitStopListener(emitter cmds.ResponseEmitter, apiName ...string) error {
	return emitListener(emitter, path.Join(
		append(
			[]string{"/api/"},
			apiName...,
		)...,
	))
}

func emitSignalListener(emitter cmds.ResponseEmitter, sig os.Signal) error {
	return emitListener(emitter, path.Join("/os/", sig.String()))
}

func emitCmdsListener(emitter cmds.ResponseEmitter) error {
	return emitListener(emitter, "/go/cmds/request")
}

func emitTickerListener(emitter cmds.ResponseEmitter, interval time.Duration, name ...string) error {
	if len(name) == 0 {
		name = []string{"anonymous"}
	}
	fullName := append(
		append(
			[]string{"/go/ticker/"},
			name...,
		),
		interval.String(),
	)
	return emitListener(emitter, path.Join(fullName...))
}
