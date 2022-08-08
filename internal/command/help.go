package command

import (
	"flag"
	"strconv"
)

type (
	// TODO: docs
	// HelpArg implements [HelpFlag].
	// (A check that looks for `-help`
	// and returns before calling [CommandFunc].)
	HelpArg bool

	HelpFlag interface {
		HelpRequested() bool
	}
)

func (b *HelpArg) BindFlags(fs *flag.FlagSet) {
	const usage = "Prints out this help text."
	fs.BoolVar((*bool)(b), "help", false, usage)
}
func (b HelpArg) HelpRequested() bool { return bool(b) }

// Returns string representation of HelpFlag's boolean value
func (b *HelpArg) String() string { return strconv.FormatBool(bool(*b)) }

// Parse boolean value from string representation and set to HelpFlag
func (b *HelpArg) Set(str string) error {
	val, err := strconv.ParseBool(str)
	if err != nil {
		return err
	}
	*b = HelpArg(val)
	return nil
}
