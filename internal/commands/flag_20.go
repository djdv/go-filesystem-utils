//go:build !go1.21

package commands

import "flag"

type boolFuncValue func(string) error

func (f boolFuncValue) Set(s string) error { return f(s) }

func (f boolFuncValue) String() string { return "" }

func (f boolFuncValue) IsBoolFlag() bool { return true }

func boolFunc(flagSet *flag.FlagSet, name, usage string, fn func(string) error) {
	flagSet.Var(boolFuncValue(fn), name, usage)
}
