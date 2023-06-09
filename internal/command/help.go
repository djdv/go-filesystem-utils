package command

import "flag"

// Help implements [HelpFlag].
type Help bool

// HelpRequested will return true if a help flag
// was set during parsing.
func (help Help) HelpRequested() bool { return bool(help) }

// BindFlags defines a `-help` flag in the [flag.FlagSet].
func (help *Help) BindFlags(fs *flag.FlagSet) {
	const usage = "prints out this help text"
	fs.BoolVar((*bool)(help), "help", false, usage)
}
