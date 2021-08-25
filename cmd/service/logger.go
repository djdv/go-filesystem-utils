package service

import (
	"fmt"

	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
)

type (
	logger interface {
		service.Logger
		Starting() error
		Listener(multiaddr.Multiaddr) error
		Ready() error
	}
	cmdsLogger    struct{ cmds.ResponseEmitter }
	serviceLogger struct{ service.Logger }
)

func newCmdsLogger(emitter cmds.ResponseEmitter) logger {
	return cmdsLogger{ResponseEmitter: emitter}
}
func newServiceLogger(svc service.Service) (logger, error) {
	sysLog, err := svc.SystemLogger(nil)
	if err != nil {
		return nil, err
	}
	return serviceLogger{Logger: sysLog}, nil
}

func (l cmdsLogger) Starting() error {
	return l.ResponseEmitter.Emit(&ipc.ServiceResponse{Status: ipc.ServiceStarting})
}

func (l cmdsLogger) Listener(maddr multiaddr.Multiaddr) error {
	return l.ResponseEmitter.Emit(&ipc.ServiceResponse{
		Status:        ipc.ServiceReady,
		ListenerMaddr: &formats.Multiaddr{Interface: maddr},
	},
	)
}

func (l cmdsLogger) Ready() error {
	return l.ResponseEmitter.Emit(&ipc.ServiceResponse{Status: ipc.ServiceReady})
}

func (l cmdsLogger) Error(v ...interface{}) error {
	return l.ResponseEmitter.Emit(&ipc.ServiceResponse{
		Status: ipc.ServiceError,
		Info:   fmt.Sprint(v...),
	})
}

func (l cmdsLogger) Errorf(format string, v ...interface{}) error {
	return l.ResponseEmitter.Emit(&ipc.ServiceResponse{
		Status: ipc.ServiceError,
		Info:   fmt.Sprintf(format, v...),
	})
}

func (l cmdsLogger) Warningf(format string, v ...interface{}) error {
	return l.ResponseEmitter.Emit(&ipc.ServiceResponse{Info: fmt.Sprintf(format, v...)})
}
func (l cmdsLogger) Warning(v ...interface{}) error {
	return l.ResponseEmitter.Emit(&ipc.ServiceResponse{Info: fmt.Sprint(v...)})
}

func (l cmdsLogger) Infof(format string, v ...interface{}) error {
	return l.ResponseEmitter.Emit(&ipc.ServiceResponse{Info: fmt.Sprintf(format, v...)})
}
func (l cmdsLogger) Info(v ...interface{}) error {
	return l.ResponseEmitter.Emit(&ipc.ServiceResponse{Info: fmt.Sprint(v...)})
}

func (l serviceLogger) Starting() error {
	return l.Logger.Info(ipc.StdHeader)
}

func (l serviceLogger) Listener(maddr multiaddr.Multiaddr) error {
	return l.Logger.Info(ipc.StdGoodStatus, maddr.String())
}

func (l serviceLogger) Ready() error {
	return l.Logger.Info(ipc.StdReady)
}
