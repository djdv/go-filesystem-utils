package daemon

import (
	"fmt"
	"os"
	"path"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/multiformats/go-multiaddr"
)

func statusResponse(status Status, stopReason environment.Reason) *Response {
	return &Response{
		Status:     status,
		StopReason: stopReason,
	}
}

func maddrListenerResponse(maddr multiaddr.Multiaddr) *Response {
	return &Response{
		Status:        Starting,
		ListenerMaddr: &formats.Multiaddr{Interface: maddr},
	}
}

func listenerResponse(name string) *Response {
	return infoResponsef("listening on: %s", name)
}

func infoResponsef(fmtStr string, v ...interface{}) *Response {
	return &Response{Info: fmt.Sprintf(fmtStr, v...)}
}

func startingResponse() *Response { return statusResponse(Starting, 0) }
func readyResponse() *Response    { return statusResponse(Ready, 0) }
func stoppingResponse(reason environment.Reason) *Response {
	return statusResponse(Stopping, reason)
}

func stopListenerResponse(apiPath ...string) *Response {
	return listenerResponse(path.Join(append(
		[]string{"/api/"},
		apiPath...,
	)...,
	))
}

func signalListenerResponse(sig os.Signal) *Response {
	return listenerResponse(path.Join("/os/", sig.String()))
}

func cmdsListenerResponse() *Response {
	return listenerResponse("/go/cmds/request")
}

func tickerListenerResponse(interval time.Duration, name ...string) *Response {
	fullName := append(
		append(
			[]string{"/go/ticker/"},
			name...,
		),
		interval.String(),
	)
	return listenerResponse(path.Join(fullName...))
}
