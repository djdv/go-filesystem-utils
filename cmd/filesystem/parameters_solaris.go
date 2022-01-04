package filesystem

import "github.com/djdv/go-filesystem-utils/cmd/parameters"

type PlatformSettings struct {
	POSIXSettings
	Prefix string
}

func (*PlatformSettings) Parameters() parameters.Parameters {
	posix := (*POSIXSettings)(nil).Parameters()
	return append(posix,
		Prefix(),
	)
}

func Prefix() parameters.Parameter {
	return parameters.NewParameter(
		"Service FMRI prefix.",
	)
}
