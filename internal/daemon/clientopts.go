package daemon

import "github.com/u-root/uio/ulog"

type (
	ClientOption   interface{ apply(*clientSettings) }
	clientSettings struct {
		protocolLogger ulog.Logger
	}

	uloggerOption struct{ ulog.Logger }

	clientLogProtocolOpt uloggerOption
)

func WithProtocolLogger(ul ulog.Logger) ClientOption { return clientLogProtocolOpt{ul} }

func (ul clientLogProtocolOpt) apply(set *clientSettings) {
	set.protocolLogger = ul.Logger
}

func parseClientOptions(options ...ClientOption) (settings clientSettings) {
	for _, opt := range options {
		opt.apply(&settings)
	}
	if settings.protocolLogger == nil {
		settings.protocolLogger = ulog.Null
	}
	return
}
