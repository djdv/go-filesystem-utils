package command

import "flag"

type (
	// HelpArg implements [HelpFlag].
	HelpArg bool

	// HelpFlag's `Help` method will signify
	// whether a caller requested help or not.
	HelpFlag interface {
		Help() bool
	}
)

// Help will return true if the help flag
// was present when parsing.
func (b HelpArg) Help() bool { return bool(b) }

// BindFlags defines a `-help` flag in the [flag.FlagSet].
func (b *HelpArg) BindFlags(fs *flag.FlagSet) {
	const usage = "prints out this help text"
	fs.BoolVar((*bool)(b), "help", false, usage)
}
