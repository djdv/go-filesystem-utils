package p9

import (
	"context"
	"fmt"
	"reflect" // [fa0f68c3-8fdc-445a-9ddf-699da39a77c2]
	"sync/atomic"
	"unsafe" // [fa0f68c3-8fdc-445a-9ddf-699da39a77c2]

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
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
	metaSettings struct {
		metadata
		withTimestamps bool
	}
	MetaOption func(*metaSettings) error

	linkSettings struct {
		parent p9.File
		name   string
	}
	LinkOption func(*linkSettings) error

	fileSettings struct {
		metaSettings
		linkSettings
	}
	FileOption func(*fileSettings) error

	directorySettings struct {
		fileSettings
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

	mounterSettings struct {
		directorySettings
		generatorSettings
	}
	MounterOption func(*mounterSettings) error

	fuseSettings struct {
		directorySettings
		generatorSettings
	}
	FuseOption func(*fuseSettings) error

	fsidSettings struct {
		directorySettings
		generatorSettings
		hostAPI filesystem.Host
	}
	FSIDOption func(*fsidSettings) error

	ipfsSettings struct {
		fileSettings
		// TODO: node addr, etc.
	}
	IPFSOption func(*ipfsSettings) error

	NineOption (func()) // TODO stub
)

func parseOptions[ST any, OT ~func(*ST) error](settings *ST, options ...OT) error {
	for _, setFunc := range options {
		if err := setFunc(settings); err != nil {
			return err
		}
	}
	return nil
}

// TODO: This needs an example code to make sense.
//
// WithSuboptions converts shared option types
// into the requested option type.
func WithSuboptions[
	NOT ~func(*NST) error, // New Option Type.
	NST, OST any, // X Settings Type.
	OOT ~func(*OST) error, // Old Option Type.
](options ...OOT,
) NOT {
	return func(s *NST) error {
		ptr, err := hackGetPtr[OST](s)
		if err != nil {
			return err
		}
		return parseOptions(ptr, options...)
	}
}

func WithPath(path *atomic.Uint64) MetaOption {
	return func(set *metaSettings) error { set.ninePath = path; return nil }
}

// TODO: docs
// Constructors may use this attr freely.
// (Fields may be ignored or modified.)
func WithBaseAttr(attr *p9.Attr) MetaOption {
	return func(set *metaSettings) error {
		/* TODO: lint; disallow this? Merge attrs? <- yeah probably.
		if existing := set.Attr; existing != nil {
			return fmt.Errorf("base attr already set:%v", existing)
		}
		*/
		set.Attr = attr
		return nil
	}
}

func WithAttrTimestamps(b bool) MetaOption {
	return func(ms *metaSettings) error { ms.withTimestamps = true; return nil }
}

// TODO: name is the name of the child, in relation to the parent, not the parent node's name.
// We need a good variable-name for this. selfName? ourName?
func WithParent(parent p9.File, name string) LinkOption {
	return func(ls *linkSettings) error { ls.parent = parent; ls.name = name; return nil }
}

// TODO: name? + docs
func CleanupSelf(b bool) GeneratorOption {
	return func(set *generatorSettings) error { set.cleanupSelf = b; return nil }
}

// TODO: name? + docs
func CleanupEmpties(b bool) GeneratorOption {
	return func(set *generatorSettings) error { set.cleanupElements = b; return nil }
}

// TODO: we should either export these settings reflectors or make a comparable function.
// Even better would be to eliminate the need for them all together.

func (settings *metaSettings) asOptions() []MetaOption {
	return []MetaOption{
		WithPath(settings.ninePath),
		WithBaseAttr(settings.Attr),
		WithAttrTimestamps(settings.withTimestamps),
	}
}

func (settings *linkSettings) asOptions() []LinkOption {
	return []LinkOption{
		WithParent(settings.parent, settings.name),
	}
}

// TODO: [Ame] Words words words. How about some concision?
// HACK: [Go 1.19] [fa0f68c3-8fdc-445a-9ddf-699da39a77c2]
// Several proposals have been accepted within Go [*1]
// which allow several possible implementations of what we're trying to do here.
// None of which have been implemented in the compiler yet.
// Until then, this is the best I could come up with.
// [*1] common struct fields; type parameters on methods; mixed concrete+interface unions; and more.
//
// The implementation is bad, but should be amendable later
// without changing the calling code.
//
// This should be done with generics in a type safe, compile-time way
// when that's possible.
func hackGetPtr[T, V any](source *V) (*T, error) {
	var (
		sourceValue = reflect.ValueOf(source).Elem()
		targetType  = reflect.TypeOf((*T)(nil)).Elem()
		ctx, cancel = context.WithCancel(context.Background())
	)
	defer cancel()
	for field := range fieldsFromStruct(ctx, sourceValue.Type()) {
		if field.Type == targetType {
			fieldVal := sourceValue.FieldByIndex(field.Index)
			if !field.IsExported() {
				return hackEscapeRuntime[T](fieldVal)
			}
			return hackAssert[T](fieldVal.Interface())
		}
	}
	// TODO: can we prevent this at compile time today (v1.19)?
	// use an "implements" interface? Probably not.
	return nil, fmt.Errorf("could not find type \"%s\" within \"%T\"",
		targetType.Name(), source,
	)
}

// XXX: [Go 1.19] [fa0f68c3-8fdc-445a-9ddf-699da39a77c2]
// Returns an instance of the field's address,
// without the runtime's read-only flag.
func hackEscapeRuntime[T any](field reflect.Value) (*T, error) {
	var (
		fieldAddr  = unsafe.Pointer(field.UnsafeAddr())
		fieldWrite = reflect.NewAt(field.Type(), fieldAddr)
	)
	return hackAssert[T](fieldWrite.Interface())
}

// TODO: [Go 1.19] [fa0f68c3-8fdc-445a-9ddf-699da39a77c2]
func hackAssert[T any](value any) (*T, error) {
	concrete, ok := value.(*T)
	if !ok {
		err := fmt.Errorf("type mismatch"+
			"\n\tgot: %T"+
			"\n\twant: %T",
			concrete, (*T)(nil),
		)
		return nil, err
	}
	return concrete, nil
}

// TODO: [micro-opt] [benchmarks]
// Considering we're searching for structs that are very likely to be top level embeds,
// we want breadth first search on structs.
// (As opposed to [reflect.VisibleFields]'s lexical-depth order, or whatever.)
// However, it's likely more effect to use slices, not channels+goroutines here.
// This code was already written for something else, and re-used/adapted here.
// (Author: djdv, Takers: anyone)

type structFields = <-chan reflect.StructField

// fieldsFromStruct returns the fields from [typ] in breadth first order.
func fieldsFromStruct(ctx context.Context, typ reflect.Type) structFields {
	out := make(chan reflect.StructField)
	go func() {
		defer close(out)
		queue := []structFields{generateFields(ctx, typ)}
		for len(queue) != 0 {
			var cur structFields
			cur, queue = queue[0], queue[1:]
			for field := range cur {
				select {
				case out <- field:
					if kind := field.Type.Kind(); kind == reflect.Struct {
						queue = append(queue, expandField(ctx, field))
					}
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

func generateFields(ctx context.Context, typ reflect.Type) structFields {
	var (
		fieldCount = typ.NumField()
		fields     = make(chan reflect.StructField, fieldCount)
	)
	go func() {
		defer close(fields)
		for i := 0; i < fieldCount; i++ {
			if ctx.Err() != nil {
				return
			}
			fields <- typ.Field(i)
		}
	}()
	return fields
}

// expandField generates fields from a field,
// and prefixes their index with their container's index.
// (I.e. received [field.Index] may be passed to [container.FieldByIndex])
func expandField(ctx context.Context, field reflect.StructField) structFields {
	embeddedFields := generateFields(ctx, field.Type)
	return prefixIndex(ctx, field.Index, embeddedFields)
}

func prefixIndex(ctx context.Context, prefix []int, fields structFields) structFields {
	prefixed := make(chan reflect.StructField, cap(fields))
	go func() {
		defer close(prefixed)
		for field := range fields {
			field.Index = append(prefix, field.Index...)
			select {
			case prefixed <- field:
			case <-ctx.Done():
				return
			}
		}
	}()
	return prefixed
}
