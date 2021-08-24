package service

import (
	"errors"
	"fmt"

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
		//Stopping() error
	}
	cmdsLogger struct {
		cmds.ResponseEmitter
		errChan chan<- error
	}
	serviceLogger struct {
		service.Logger
		errChan chan<- error
	}
)

func newCmdsLogger(emitter cmds.ResponseEmitter, errs chan<- error) logger {
	return cmdsLogger{ResponseEmitter: emitter, errChan: errs}
}
func newServiceLogger(svc service.Service, errs chan<- error) (logger, error) {
	sysLog, err := svc.SystemLogger(errs)
	if err != nil {
		return nil, err
	}
	return serviceLogger{Logger: sysLog, errChan: errs}, nil
}

func (l cmdsLogger) Starting() error {
	return l.ResponseEmitter.Emit(&Response{Status: Starting})
}

func (l cmdsLogger) Listener(maddr multiaddr.Multiaddr) error {
	return l.ResponseEmitter.Emit(&Response{Status: Ready, ListenerMaddr: maddr})
}

func (l cmdsLogger) Ready() error {
	return l.ResponseEmitter.Emit(&Response{Status: Ready})
}

func (l cmdsLogger) Stopping() error {
	//return l.ResponseEmitter.Emit(&Response{Status: Stopping})
	return nil
}

func (l cmdsLogger) Error(v ...interface{}) error {
	if len(v) == 1 {
		if err, isError := v[0].(error); isError {
			l.errChan <- err
			return nil
		}
	}
	l.errChan <- errors.New(fmt.Sprint(v...))
	return nil
}

func (l cmdsLogger) Errorf(format string, v ...interface{}) error {
	l.errChan <- fmt.Errorf(format, v...)
	return nil
}

func (l cmdsLogger) Warningf(format string, v ...interface{}) error {
	return l.ResponseEmitter.Emit(&Response{Info: fmt.Sprintf(format, v...)})
}
func (l cmdsLogger) Warning(v ...interface{}) error {
	return l.ResponseEmitter.Emit(&Response{Info: fmt.Sprint(v...)})
}

func (l cmdsLogger) Infof(format string, v ...interface{}) error {
	return l.ResponseEmitter.Emit(&Response{Info: fmt.Sprintf(format, v...)})
}
func (l cmdsLogger) Info(v ...interface{}) error {
	return l.ResponseEmitter.Emit(&Response{Info: fmt.Sprint(v...)})
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

func (l serviceLogger) Stopping() error {
	return l.Logger.Info("Stopping...")
}

func (l serviceLogger) Error(v ...interface{}) error {
	err := errors.New(fmt.Sprint(v...))
	lErr := l.Logger.Error(err)
	l.errChan <- err
	return lErr
}

func (l serviceLogger) Errorf(format string, v ...interface{}) error {
	err := fmt.Errorf(format, v...)
	lErr := l.Logger.Error(err)
	l.errChan <- err
	return lErr
}
