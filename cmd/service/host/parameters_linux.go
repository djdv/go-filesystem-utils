package host

import "github.com/djdv/go-filesystem-utils/internal/parameters"

type PlatformSettings struct {
	POSIXSettings
	OpenRCScript string
	LimitNOFILE  int
}

func (*PlatformSettings) Parameters() parameters.Parameters {
	posix := (*POSIXSettings)(nil).Parameters()
	return append(posix,
		OpenRCScript(),
		LimitNOFILE(),
	)
}

func OpenRCScript() parameters.Parameter {
	return parameters.NewParameter(
		"Use custom OpenRCS script",
	)
}

func LimitNOFILE() parameters.Parameter {
	return parameters.NewParameter(
		"Maximum open files (ulimit -n) (https://serverfault.com/questions/628610/increasing-nproc-for-processes-launched-by-systemd-on-centos-7)",
	)
}
