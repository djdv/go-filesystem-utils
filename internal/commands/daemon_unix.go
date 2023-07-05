//go:build unix

package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func registerSystemStoppers(_ context.Context, shutdownSend wgShutdown) {
	shutdownSend.Add(2)
	go stopOnSignalLinear(shutdownSend, os.Interrupt)
	go stopOnSignalLinear(shutdownSend, syscall.SIGTERM)
}

func stopOnSignalLinear(stopCh wgShutdown, sig os.Signal) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, sig)
	defer func() {
		signal.Stop(signals)
		stopCh.Done()
	}()
	for count := minimumShutdown; count <= maximumShutdown; count++ {
		select {
		case <-signals:
			if !stopCh.send(count) {
				return
			}
		case <-stopCh.Closing():
			return
		}
	}
}
