package host

import (
	"github.com/djdv/go-filesystem-utils/internal/cmdslib"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type Settings struct {
	Username string `settings:"arguments"`
	ServiceName,
	ServiceDisplayName,
	ServiceDescription string

	PlatformSettings
}

func (*Settings) Parameters() parameters.Parameters {
	return append([]parameters.Parameter{
		Username(),
		ServiceName(),
		ServiceDisplayName(),
		ServiceDescription(),
	},
		(*PlatformSettings)(nil).Parameters()...,
	)
}

func Username() parameters.Parameter {
	return cmdslib.NewParameter(
		"Username to use when interfacing with the system service manager.",
	)
}

func ServiceName() parameters.Parameter {
	return cmdslib.NewParameter(
		"Service name (usually as a command argument) to associate with the service (when installing)",
	)
}

func ServiceDisplayName() parameters.Parameter {
	return cmdslib.NewParameter(
		"Service display name (usually seen in UI labels) to associate with the service (when installing)",
	)
}

func ServiceDescription() parameters.Parameter {
	return cmdslib.NewParameter(
		"Description (usually seen in UI labels) to associate with the service (when installing)",
	)
}
