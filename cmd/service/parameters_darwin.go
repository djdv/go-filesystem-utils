package service

import "github.com/djdv/go-filesystem-utils/internal/parameters"

type PlatformSettings struct {
	POSIXSettings
	LaunchdConfig,
	SocketsType,
	SocketsPathName string
	SocketsPathMode int
	KeepAlive,
	RunAtLoad,
	SessionCreate bool
}

func (*PlatformSettings) Parameters() parameters.Parameters {
	posix := (*POSIXSettings)(nil).Parameters()
	return append(posix,
		LaunchdConfig(),
		SocketsType(),
		SocketsPathName(),
		SocketsPathMode(),
		KeepAlive(),
		RunAtLoad(),
		SessionCreate(),
	)
}

func LaunchdConfig() parameters.Parameter {
	return parameters.NewParameter(
		"Use custom launchd config",
	)
}

func KeepAlive() parameters.Parameter {
	return parameters.NewParameter(
		" Prevent the system from stopping the service automatically.",
	)
}

func RunAtLoad() parameters.Parameter {
	return parameters.NewParameter(
		"Run the service after its job has been loaded.",
	)
}

func SessionCreate() parameters.Parameter {
	return parameters.NewParameter(
		"Create a full user session.",
	)
}

func SocketsType() parameters.Parameter {
	return parameters.NewParameter(
		"...",
	)
}

func SocketsPathName() parameters.Parameter {
	return parameters.NewParameter(
		"...",
	)
}

func SocketsPathMode() parameters.Parameter {
	return parameters.NewParameter(
		"...",
	)
}
