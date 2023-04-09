package p9

// TODO: some way to provide statfs for files that are themselves,
// not devices, but hosted inside one.
//
// Implementations should probably have a default of `0x01021997` (V9FS_MAGIC) for `f_type`
// Or we can make up our own magic numbers (something not already in use)
// to guarantee we're not misinterpreted (as a FS that we're not)
// by callers / the OS (Linux specifically).
//
// The Linux manual has this to say about `f_fsid`
// "Nobody knows what f_fsid is supposed to contain" ...
// we'll uhhh... figure something out later I guess.

type (
	// TODO: docs; commonly shared options.
	Options interface {
		DirectoryOption |
			ListenerOption |
			ChannelOption |
			MounterOption |
			HosterOption |
			FSIDOption |
			MountPointOption
	}
	// TODO: docs; systems which generate files.
	GeneratorOptions interface {
		ListenerOption |
			MounterOption |
			HosterOption |
			FSIDOption
	}
	fileOptions struct {
		metaOptions []metadataOption
		linkOptions []linkOption
	}

	channelSettings struct {
		metadata
		linkSettings
		buffer int
	}
	ChannelOption func(*channelSettings) error

	NineOption (func()) // TODO stub
)
