package command

import (
	"flag"
	"strconv"
)

type (
	// HelpArg implements [HelpFlag].
	HelpArg bool

	// HelpFlag providers a getter for a `-help` flag.
	HelpFlag interface {
		Help() bool
	}
)

func (b HelpArg) Help() bool      { return bool(b) }
func (b *HelpArg) String() string { return strconv.FormatBool(bool(*b)) }

// BindFlags defines a `-help` flag in the [flag.FlagSet].
func (b *HelpArg) BindFlags(fs *flag.FlagSet) {
	const usage = "Prints out this help text."
	fs.BoolVar((*bool)(b), "help", false, usage)
}

// Set parses boolean strings into [HelpArg].
func (b *HelpArg) Set(str string) error {
	val, err := strconv.ParseBool(str)
	if err != nil {
		return err
	}
	*b = HelpArg(val)
	return nil
}
