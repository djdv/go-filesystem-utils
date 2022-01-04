package filesystem

import "github.com/djdv/go-filesystem-utils/cmd/parameters"

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
	return parameters.NewParameter(
		"Password to use when interfacing with the system service manager.",
	)
}

func DelayedAutoStart() parameters.Parameter {
	return parameters.NewParameter(
		"Prevent the service from starting immediately after booting.",
	)
}
