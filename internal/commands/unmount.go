package commands

import (
	"flag"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/commands/unmount"
)

type unmountOptions []unmount.Option

// Unmount constructs the [command.Command]
// which requests the file system service
// to undo the effects of a previous mount.
func Unmount() command.Command {
	const (
		name     = "unmount"
		synopsis = "Unmount file systems."
	)
	usage := heading("Unmount") +
		"\n\n" + synopsis +
		"\nAccepts mountpoints as arguments."
	return command.MakeVariadicCommand[unmountOptions](
		name, synopsis, usage,
		unmount.Detach,
	)
}

func (uo *unmountOptions) BindFlags(flagSet *flag.FlagSet) {
	uo.bindClientFlags(flagSet)
	uo.bindAllFlag(flagSet)
}

func (uo *unmountOptions) bindClientFlags(flagSet *flag.FlagSet) {
	inheritClientFlags(
		flagSet, uo, nil, // No default client options.
		unmount.WithClientOptions,
	)
}

func (uo *unmountOptions) bindAllFlag(flagSet *flag.FlagSet) {
	const (
		name  = "all"
		usage = "unmount all"
	)
	transformFn := func(all bool) unmount.Option {
		return unmount.All(all)
	}
	insertSliceOnce(
		flagSet, name, usage,
		uo, strconv.ParseBool, transformFn,
	)
}
