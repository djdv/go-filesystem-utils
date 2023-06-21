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
	fileSettings struct {
		linkSync
		metadata
	}
	optionFunc[T any] interface {
		~func(*T) error
	}
)

func applyOptions[OT optionFunc[T], T any](settings *T, options ...OT) error {
	for _, apply := range options {
		if err := apply(settings); err != nil {
			return err
		}
	}
	return nil
}
