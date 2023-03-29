package p9

import (
	"reflect"
	"sync/atomic"
	"unsafe"

	"github.com/hugelgupf/p9/p9"
)

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

	linkSettings = link

	directorySettings struct {
		metadata
		linkSettings
	}
	DirectoryOption func(*directorySettings) error

	generatorSettings struct {
		cleanupSelf     bool // TODO: better name? Different container?
		cleanupElements bool // TODO: better name? cleanupItems?
	}
	GeneratorOption func(*generatorSettings) error

	listenerSettings struct {
		directorySettings
		generatorSettings
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

	reflectFunc = func([]reflect.Value) (results []reflect.Value)
)

func parseOptions[ST any, OT ~func(*ST) error](settings *ST, options ...OT) error {
	for _, setFunc := range options {
		if err := setFunc(settings); err != nil {
			return err
		}
	}
	return nil
}

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

// XXX: We're using reflection to work around
// a constraint in the 1.18 Go spec. Specifically regarding
// generic access to common struct fields.
// "We may remove this restriction in a future release"
// It's possible to implement this without reflection
// today [1.20], but it requires a lot of duplication
// type switch cases. (1 for each type for each option).
// We'll take the runtime hit until the aforementioned
// compiler constraint is renounced.
func makeSetter[OT Options, V any](name string, value V) OT {
	optTyp := getOptionType[OT]()
	return makeReflectFn[OT, V](optTyp,
		func(args []reflect.Value) (results []reflect.Value) {
			fieldPtr := unsafeFieldAccess(args[0], name)
			fieldPtr.Elem().Set(reflect.ValueOf(value))
			return []reflect.Value{reflect.Zero(optTyp.Out(0))}
		},
	)
}

func makeSetterFn[OT Options, V any](name string, fn func(*V) error) OT {
	optTyp := getOptionType[OT]()
	return makeReflectFn[OT, V](optTyp,
		func(args []reflect.Value) (results []reflect.Value) {
			var (
				fieldPtr = unsafeFieldAccess(args[0], name)
				fnRet    = fn(fieldPtr.Interface().(*V))
				rvRet    = reflect.ValueOf(&fnRet).Elem()
			)
			return []reflect.Value{rvRet}
		},
	)
}

// XXX: defeat CanSet/CanAddr guard for unexported fields.
func unsafeFieldAccess(structPtr reflect.Value, name string) reflect.Value {
	var (
		field   = structPtr.Elem().FieldByName(name)
		srcAddr = unsafe.Pointer(field.UnsafeAddr())
	)
	return reflect.NewAt(field.Type(), srcAddr)
}

func makeReflectFn[OT Options, V any](optTyp reflect.Type, fn reflectFunc) OT {
	return reflect.MakeFunc(optTyp, fn).Interface().(OT)
}

func getOptionType[OT Options]() reflect.Type {
	return reflect.TypeOf([0]OT{}).Elem()
}

func WithPath[OT Options](path *atomic.Uint64) (option OT) {
	return makeSetter[OT]("ninePath", path)
}

func WithParent[OT Options](parent p9.File, child string) (option OT) {
	return makeSetter[OT]("linkSettings", linkSettings{
		parent: parent,
		child:  child,
	})
}

func WithPermissions[OT Options](permissions p9.FileMode) (option OT) {
	return makeSetterFn[OT]("Mode", func(mode *p9.FileMode) error {
		*mode = mode.FileType() | permissions.Permissions()
		return nil
	})
}

func WithUID[OT Options](uid p9.UID) (option OT) {
	return makeSetter[OT]("UID", uid)
}

func WithGID[OT Options](gid p9.GID) (option OT) {
	return makeSetter[OT]("GID", gid)
}

func UnlinkWhenEmpty[OT GeneratorOptions](b bool) (option OT) {
	return makeSetter[OT]("cleanupSelf", b)
}

func UnlinkEmptyChildren[OT GeneratorOptions](b bool) (option OT) {
	return makeSetter[OT]("cleanupElements", b)
}

func WithBuffer(size int) ChannelOption {
	return func(cs *channelSettings) error { cs.buffer = size; return nil }
}
