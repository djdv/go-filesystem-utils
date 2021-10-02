package fscmds

// TODO: I don't know if this belongs here, or is a good idea at all.
// It's better than having multiple manually coded sections, but kind of weird.
//
// DaemonCmdsPath returns the leading parameters
// to invoke the daemon's `Run` method from `main`.
func DaemonCmdsPath() []string { return []string{"service", "daemon"} }
