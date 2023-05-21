//go:build go1.21

package commands

import "flag"

func boolFunc(flagSet *flag.FlagSet, name, usage string, fn func(string) error) {
	flagSet.BoolFunc(name, usage, fn)
}
