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
	ListenerOption func(*listenerSettings) error

	fsidSettings struct {
		directorySettings
		generatorSettings
	}
	FSIDOption func(*fsidSettings) error

	channelSettings struct {
		metadata
		linkSettings
		buffer int
	}
	ChannelOption func(*channelSettings) error

	NineOption (func()) // TODO stub
)

// TODO: See if we can coerce the type system
// to allow us to return a union of specific options.
// So we can pass them through instead of reconstructing them.
// I.e. someGeneratorOption() (f1|f2, error)
// parsing would need some special handling to accrue them.
//
// alternatively we could store a slice of them
// on the settings type itself.
// someGeneratoreSettings.[]diropts
// ^ This might cause problems for things like the path.
func (settings *directorySettings) asOptions() []DirectoryOption {
	return []DirectoryOption{
		WithPath[DirectoryOption](settings.ninePath),
		WithPermissions[DirectoryOption](settings.Mode),
		WithUID[DirectoryOption](settings.UID),
		WithGID[DirectoryOption](settings.GID),
		WithParent[DirectoryOption](settings.parent, settings.child),
	}
}

func WithPath[OT Options](path *atomic.Uint64) (option OT) {
	return makeFieldSetter[OT]("ninePath", path)
}

func WithParent[OT Options](parent p9.File, child string) (option OT) {
	return makeFieldSetter[OT]("linkSettings", linkSettings{
		parent: parent,
		child:  child,
	})
}

func UnlinkWhenEmpty[OT GeneratorOptions](b bool) (option OT) {
	return makeFieldSetter[OT]("cleanupSelf", b)
}

func UnlinkEmptyChildren[OT GeneratorOptions](b bool) (option OT) {
	return makeFieldSetter[OT]("cleanupElements", b)
}

func WithBuffer(size int) ChannelOption {
	return func(cs *channelSettings) error { cs.buffer = size; return nil }
}
