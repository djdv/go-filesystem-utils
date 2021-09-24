package service

func systemListeners(maddrsProvided bool) (serviceListeners []manet.Listener,
	cleanup func() error, err error) {

	// TODO: pull from service config keyvalue
	socketPath = "/var/run/fsservice"
}
