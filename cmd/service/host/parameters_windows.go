package host

import (
	"github.com/djdv/go-filesystem-utils/internal/cmdslib"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type PlatformSettings struct {
	ServicePassword  string
	DelayedAutoStart bool
}

func (*PlatformSettings) Parameters() parameters.Parameters {
	return parameters.Parameters{
		ServicePassword(),
		DelayedAutoStart(),
	}
}

func ServicePassword() parameters.Parameter {
	return cmdslib.NewParameter(
		"Password to use when interfacing with the system service manager.",
	)
}

func DelayedAutoStart() parameters.Parameter {
	return cmdslib.NewParameter(
		"Prevent the service from starting immediately after booting.",
	)
}
