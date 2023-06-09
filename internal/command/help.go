package command

import "flag"

// HelpArg implements [HelpFlag].
type HelpArg bool

// HelpRequested will return true if a help flag
// was set during parsing.
func (b HelpArg) HelpRequested() bool { return bool(b) }

// BindFlags defines a `-help` flag in the [flag.FlagSet].
func (b *HelpArg) BindFlags(fs *flag.FlagSet) {
	const usage = "prints out this help text"
	fs.BoolVar((*bool)(b), "help", false, usage)
}
