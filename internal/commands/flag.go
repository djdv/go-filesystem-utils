package commands

import (
	"flag"
	"fmt"
	"io/fs"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

type (
	// optionsReference is any
	// `*optionSlice`.
	optionsReference[
		OS optionSlice[OT, T],
		OT generic.OptionFunc[T],
		T any,
	] interface {
		*OS
	}
	// optionSlice is any
	// `[]generic.OptionFunc`.
	optionSlice[
		OT generic.OptionFunc[T],
		T any,
	] interface {
		~[]OT
	}
	// settingsConstructor is any [optionSlice]
	// that can utilize itself to construct `T`
	// via a `make` method.
	settingsConstructor[
		option generic.OptionFunc[T],
		T any,
	] interface {
		optionSlice[option, T]
		make() (T, error)
	}
	// optionBinder is any [optionsReference]
	// that can bind itself to a [flag.FlagSet].
	optionBinder[
		constructor settingsConstructor[option, T],
		option generic.OptionFunc[T],
		T any,
	] interface {
		optionsReference[constructor, option, T]
		command.FlagBinder
	}
	// genericFuncValue extends [flag.funcValue]
	// to add [command.ValueNamer] support.
	// (Because standard uses internal types
	// in a way we can't access;
	// see: [flag.UnquoteUsage]'s implementation.)
	genericFuncValue[T any] func(string) error

	// genericBoolFuncValue implements [flag.Value]'s
	// `isBoolFlag` extension.
	genericBoolFuncValue[T any] struct{ genericFuncValue[T] }
)

func (gf genericFuncValue[T]) Set(s string) error { return gf(s) }
func (gf genericFuncValue[T]) String() string     { return "" }
func (gf genericFuncValue[T]) Name() string {
	name := reflect.TypeOf([0]T{}).Elem().String()
	if index := strings.LastIndexByte(name, '.'); index != -1 {
		name = name[index+1:] // Remove [QualifiedIdent] prefix.
	}
	return strings.ToLower(name)
}

func (gb genericBoolFuncValue[T]) IsBoolFlag() bool { return true }

const (
	permMaximum    = 0o7777
	permReadAll    = 0o444
	permWriteAll   = 0o222
	permExecuteAll = 0o111
	permUserBits   = os.ModeSticky | os.ModeSetuid | 0o700
	permGroupBits  = os.ModeSetgid | 0o070
	permOtherBits  = 0o007
	permSetid      = fs.ModeSetuid | fs.ModeSetgid
	permAllBits    = permUserBits | permGroupBits | permOtherBits
	permOpAdd      = '+'
	permOpSub      = '-'
	permOpSet      = '='
	permWhoUser    = 'u'
	permWhoGroup   = 'g'
	permWhoOther   = 'o'
	permWhoAll     = 'a'
	permSymRead    = 'r'
	permSymWrite   = 'w'
	permSymExecute = 'x'
	permSymSearch  = 'X'
	permSymSetID   = 's'
	permSymText    = 't'
)

func makeWithOptions[OT generic.OptionFunc[T], T any](options ...OT) (T, error) {
	var settings T
	return settings, generic.ApplyOptions(&settings, options...)
}

func parseID[id fuseID | p9.UID | p9.GID](arg string) (id, error) {
	const nobody = "nobody"
	if arg == nobody {
		var value id
		switch any(value).(type) {
		case p9.UID:
			value = id(p9.NoUID)
		case p9.GID:
			value = id(p9.NoGID)
		case fuseID:
			value = id(math.MaxInt32)
		}
		return value, nil
	}
	const idSize = 32
	num, err := strconv.ParseUint(arg, 0, idSize)
	if err != nil {
		return 0, err
	}
	return id(num), nil
}

func idString[id uint32 | p9.UID | p9.GID](who id) string {
	const nobody = "nobody"
	switch typed := any(who).(type) {
	case p9.UID:
		if typed == p9.NoUID {
			return nobody
		}
	case p9.GID:
		if typed == p9.NoGID {
			return nobody
		}
	}
	return strconv.Itoa(int(who))
}

func parseShutdownLevel(level string) (shutdownDisposition, error) {
	return generic.ParseEnum(minimumShutdown, maximumShutdown, level)
}

// parsePOSIXPermissions accepts a `chmod` "mode" parameter
// (as defined in SUSv4;BSi7), and returns the result of
// applying it to the `mode` value.
func parsePOSIXPermissions(mode fs.FileMode, clauses string) (fs.FileMode, error) {
	// NOTE: The POSIX specification uses ASCII,
	// and so does the current version of this parser.
	// As a result, Unicode digits for octals and
	// any alternate symbol forms - are not supported.
	const (
		base = 8
		bits = int(unsafe.Sizeof(fs.FileMode(0))) * base
	)
	if value, err := strconv.ParseUint(clauses, base, bits); err == nil {
		if value > permMaximum {
			return 0, fmt.Errorf(`%w: "%s" exceeds permission bits boundary (%o)`,
				strconv.ErrSyntax, clauses, permMaximum)
		}
		return parseOctalPermissions(mode, fs.FileMode(value)), nil
	}
	return evalPermissionClauses(
		mode,
		parseOctalPermissions(0, getUmask()),
		strings.Split(clauses, ","),
	)
}

func parseOctalPermissions(mode, operand fs.FileMode) fs.FileMode {
	const (
		posixSuid = 0o4000
		posixSgid = 0o2000
		posixText = 0o1000
	)
	var (
		explicitHighBits bool
		permissions      = operand.Perm()
	)
	for _, pair := range [...]struct {
		posix, golang fs.FileMode
	}{
		{
			posix:  posixSuid,
			golang: fs.ModeSetuid,
		},
		{
			posix:  posixSgid,
			golang: fs.ModeSetgid,
		},
		{
			posix:  posixText,
			golang: fs.ModeSticky,
		},
	} {
		if operand&pair.posix != 0 {
			permissions |= pair.golang
			explicitHighBits = true
		}
	}
	// SUSv4;BSi7 Extended description;
	// sentence directly preceding octal table.
	if mode.IsDir() && !explicitHighBits {
		permissions |= mode & (permSetid | fs.ModeSticky)
	}
	return mode.Type() | permissions
}

func evalPermissionClauses(mode, umask fs.FileMode, clauses []string) (fs.FileMode, error) {
	for _, clause := range clauses {
		if clause == "" {
			return 0, generic.ConstError("empty clause")
		}
		remainder, impliedAll, whoMask := parseWho(clause)
		for len(remainder) != 0 {
			var (
				op          rune
				permissions fs.FileMode
				err         error
			)
			remainder, op, permissions, err = evalOp(remainder, mode)
			if err != nil {
				return 0, err
			}
			mode = applyOp(
				impliedAll, whoMask,
				mode, permissions,
				umask, op,
			)
		}
	}
	return mode, nil
}

func parseWho(clause string) (string, bool, fs.FileMode) {
	var (
		index int
		mask  fs.FileMode
	)
out:
	for _, who := range clause {
		switch who {
		case permWhoUser:
			mask |= permUserBits
		case permWhoGroup:
			mask |= permGroupBits
		case permWhoOther:
			mask |= permOtherBits
		case permWhoAll:
			mask = permAllBits
		default:
			break out
		}
		index++
	}
	// Distinguish between explicit and implied "all".
	// SUSv4;BSi7 - Operation '=', sentence 3-4.
	var impliedAll bool
	if mask == 0 {
		impliedAll = true
		mask = permAllBits
	}
	return clause[index:], impliedAll, mask
}

func evalOp(operation string, mode fs.FileMode) (string, rune, fs.FileMode, error) {
	op, operand, err := parseOp(operation)
	if err != nil {
		return "", 0, 0, err
	}
	remainder, permissions, err := parsePermissions(mode, operand)
	return remainder, op, permissions, err
}

func parseOp(clauseOp string) (rune, string, error) {
	switch op, _ := utf8.DecodeRuneInString(clauseOp); op {
	case permOpAdd, permOpSub, permOpSet:
		const opOffset = 1 // WARN: ASCII-ism.
		return op, clauseOp[opOffset:], nil
	default:
		return 0, "", fmt.Errorf("missing op symbol, got: %c", op)
	}
}

func parsePermissions(mode fs.FileMode, clauseOperand string) (string, fs.FileMode, error) {
	remainder, copied, permissions := parsePermcopy(mode, clauseOperand)
	if copied {
		return remainder, permissions, nil
	}
	var (
		index int
		bits  os.FileMode
	)
	for _, perm := range clauseOperand {
		switch perm {
		case permSymRead:
			bits |= permReadAll
		case permSymWrite:
			bits |= permWriteAll
		case permSymExecute:
			bits |= permExecuteAll
		case permSymSearch:
			// SUSv4;BSi7 Extended description paragraph 5.
			if mode.IsDir() ||
				(mode&permExecuteAll != 0) {
				bits |= permExecuteAll
			}
		case permSymSetID:
			bits |= permSetid
		case permSymText:
			bits |= fs.ModeSticky
		case permOpAdd, permOpSub, permOpSet:
			return clauseOperand[index:], bits, nil
		default:
			return "", 0, fmt.Errorf("unexpected perm symbol: %c", perm)
		}
		index++
	}
	return clauseOperand[index:], bits, nil
}

func parsePermcopy(mode fs.FileMode, clauseFragment string) (string, bool, fs.FileMode) {
	if len(clauseFragment) == 0 {
		return "", false, 0
	}
	const (
		groupShift = 3
		userShift  = 6
	)
	var permissions fs.FileMode
	switch who, _ := utf8.DecodeRuneInString(clauseFragment); who {
	case permWhoUser:
		permissions = (mode & permUserBits) >> userShift
	case permWhoGroup:
		permissions = (mode & permGroupBits) >> groupShift
	case permWhoOther:
		permissions = (mode & permOtherBits)
	default:
		return "", false, 0
	}
	// Replicate the permission bits to all fields.
	// (Caller is expected to mask against "who".)
	permissions |= (permissions << groupShift) |
		(permissions << userShift)
	return clauseFragment[1:], true, permissions
}

func applyOp(impliedAll bool,
	who, mode, mask, umask fs.FileMode, op rune,
) fs.FileMode {
	mask &= who
	if impliedAll {
		mask &^= umask
	}
	switch op {
	case '+':
		mode |= mask
	case '-':
		mode &^= mask
	case '=':
		// SUSv4;BSi7 says set-*-ID bit handling for non-regular files
		// is implementation-defined.
		// Most unices seem to preserve set-*-ID bits for directories.
		if mode.IsDir() {
			mask |= (mode & permSetid)
		}
		mode = (mode &^ who) | mask
	}
	return mode
}

func modeFromFS(mode fs.FileMode) p9.FileMode {
	const (
		linuxSuid = 0o4000
		linuxSgid = 0o2000
	)
	// NOTE: [2023.05.20]
	// Upstream library drops bits `0o7000`
	// in this call. Since we (currently) use
	// 9P2000.L and these bits are valid, we add
	// them back in if present.
	mode9 := p9.ModeFromOS(mode)
	for _, pair := range [...]struct {
		plan9  p9.FileMode
		golang fs.FileMode
	}{
		{
			plan9:  linuxSuid,
			golang: fs.ModeSetuid,
		},
		{
			plan9:  linuxSgid,
			golang: fs.ModeSetgid,
		},
		{
			plan9:  p9.Sticky,
			golang: fs.ModeSticky,
		},
	} {
		if mode&pair.golang != 0 {
			mode9 |= pair.plan9
		}
	}
	return mode9
}

func modeToSymbolicPermissions(mode fs.FileMode) string {
	const (
		prefix    = 2 // u=
		maxCell   = 4 // rwxs
		separator = 1 // ,
		groups    = 3 // u,g,o
		maxSize   = ((prefix + maxCell) * groups) + (separator * (groups - 1))
	)
	var (
		sb    strings.Builder
		pairs = []struct {
			whoMask, specMask     fs.FileMode
			whoSymbol, specSymbol rune
		}{
			{
				whoMask:    permUserBits,
				whoSymbol:  permWhoUser,
				specMask:   fs.ModeSetuid,
				specSymbol: permSymSetID,
			},
			{
				whoMask:    permGroupBits,
				whoSymbol:  permWhoGroup,
				specMask:   fs.ModeSetgid,
				specSymbol: permSymSetID,
			},
			{
				whoMask:    permOtherBits,
				whoSymbol:  permWhoOther,
				specMask:   fs.ModeSticky,
				specSymbol: permSymText,
			},
		}
	)
	sb.Grow(maxSize)
	var previousLen int
	for i, pair := range pairs {
		writePermSymbols(&sb, mode, pair.whoMask, pair.specMask, pair.whoSymbol, pair.specSymbol)
		if i != len(pairs)-1 && sb.Len() != previousLen {
			sb.WriteByte(',')
		}
		previousLen = sb.Len() // No writes, no separator.
	}
	return sb.String()
}

func writePermSymbols(sb *strings.Builder, mode, who, special fs.FileMode, whoSym, specSym rune) {
	var (
		filtered    = mode & who
		haveSpecial = mode&special != 0
		runes       []rune
		pairs       = []struct {
			mask   fs.FileMode
			symbol rune
		}{
			{
				mask:   permReadAll,
				symbol: permSymRead,
			},
			{
				mask:   permWriteAll,
				symbol: permSymWrite,
			},
			{
				mask:   permExecuteAll,
				symbol: permSymExecute,
			},
		}
	)
	for _, pair := range pairs {
		if filtered&pair.mask != 0 {
			runes = append(runes, pair.symbol)
		}
	}
	if len(runes) == 0 && !haveSpecial {
		return
	}
	sb.WriteRune(whoSym)
	sb.WriteByte('=')
	for _, r := range runes {
		sb.WriteRune(r)
	}
	if haveSpecial {
		sb.WriteRune(specSym)
	}
}

func header(text string) string {
	return "# " + text
}

func underline(text string) string {
	return fmt.Sprintf(
		"%s\n%s",
		text,
		strings.Repeat("-", len(text)),
	)
}

// appendFlagValue uses `parseFn` and `getRefFn`,
// to create a [flag.Value] which will be
// bound to the `flagSet`.
// `parseFn` translates string input arguments
// into a typed Go value.
// `getRefFn` returns an address where the
// parsed value, should be written to.
// An option value will be appended to
// `optionsPtr` upon a successful parse.
func appendFlagValue[
	optionsRef optionsReference[options, optionT, settingsT],
	options optionSlice[optionT, settingsT],
	optionT generic.OptionFunc[settingsT],
	parseFunc func(string) (valueT, error),
	getRefFunc func(*settingsT) *valueT,
	settingsT, valueT any,
](flagSet *flag.FlagSet, name, usage string,
	optionsPtr optionsRef, parseFn parseFunc, getRefFn getRefFunc,
) {
	assignFn := func(settings *settingsT, value valueT) error {
		valuePtr := getRefFn(settings)
		*valuePtr = value
		return nil
	}
	appendFlagOption(flagSet, name, usage,
		optionsPtr, parseFn, assignFn)
}

// appendFlagList uses `parseFn` and `getRefFn`,
// to create a [flag.Value] which will be
// bound to the `flagSet`.
// `parseFn` translates string input arguments
// into a typed Go value.
// `getRefFn` returns an address to a slice,
// where the parsed value gets appended to.
// An option value will be appended to
// `optionsPtr` upon a successful parse.
func appendFlagList[optionsRef optionsReference[options, optionT, settingsT],
	options optionSlice[optionT, settingsT],
	optionT generic.OptionFunc[settingsT],
	parseFunc ~func(string) (valueT, error) |
		~func(string) ([]valueT, error),
	getRefFunc func(*settingsT) *[]valueT,
	settingsT, valueT any,
](flagSet *flag.FlagSet, name, usage string,
	optionsPtr optionsRef, parseFn parseFunc, getRefFn getRefFunc,
) {
	switch parseFn := any(parseFn).(type) {
	case func(string) (valueT, error):
		assignFn := func(settings *settingsT, typed valueT) error {
			valuesPtr := getRefFn(settings)
			*valuesPtr = append(*valuesPtr, typed)
			return nil
		}
		appendFlagOption(flagSet, name, usage,
			optionsPtr, parseFn, assignFn)
	case func(string) ([]valueT, error):
		assignFn := func(settings *settingsT, typed []valueT) error {
			valuesPtr := getRefFn(settings)
			*valuesPtr = append(*valuesPtr, typed...)
			return nil
		}
		appendFlagOption(flagSet, name, usage,
			optionsPtr, parseFn, assignFn)
	}
}

// appendFlagOption uses `parseFn` `assignFn`
// to create a [flag.Value] which will be
// bound to the `flagSet`.
// `parseFn` translates string input arguments
// into a typed Go value.
// `assignFn` receives a `*settingsT`, and the parsed value;
// where it is responsible for validating, transforming,
// assigning, etc. the value.
// An option value will be appended to
// `optionsPtr` upon a successful parse.
// `assignFn`'s error value it returned by this appended option.
func appendFlagOption[
	optionsRef optionsReference[options, optionT, settingsT],
	options optionSlice[optionT, settingsT],
	optionT generic.OptionFunc[settingsT],
	parseFunc func(string) (valueT, error),
	assignFunc func(*settingsT, valueT) error,
	settingsT, valueT any,
](flagSet *flag.FlagSet, name, usage string,
	optionsPtr optionsRef, parseFn parseFunc, assignFn assignFunc,
) {
	var (
		parseFlag = func(argument string) error {
			typed, err := parseFn(argument)
			if err != nil {
				return err
			}
			*optionsPtr = append(
				*optionsPtr,
				func(settings *settingsT) error {
					return assignFn(settings, typed)
				},
			)
			return nil
		}
		value = newFlagValue[valueT](parseFlag)
	)
	flagSet.Var(value, name, usage)
}

func newFlagValue[T any](parseFn func(string) error) flag.Value {
	valueFn := genericFuncValue[T](parseFn)
	// `bool` flags don't require a value and this
	// must be conveyed to the [flag] package.
	if _, isBool := any([0]T{}).([0]bool); !isBool {
		return valueFn
	}
	return genericBoolFuncValue[T]{
		genericFuncValue: valueFn,
	}
}

func newPassthroughFunc(name string) func(string) (string, error) {
	return func(argument string) (string, error) {
		if argument != "" {
			return argument, nil
		}
		return "", fmt.Errorf(
			"flag `-%s` had empty value",
			name,
		)
	}
}

func prefixIDFlag(system filesystem.ID) string {
	return strings.ToLower(string(system)) + "-"
}

// extendFlagSet will initialize a base [command.FlagBinder],
// which is bound to the `flagSet`.
// A function which calls the base's constructor,
// and assigns it to the address returned from `getRefFn`;
// will be appended to `options`, as a [generic.OptionFunc].
func extendFlagSet[
	binder optionBinder[constructor, baseOption, baseT],
	constructor settingsConstructor[baseOption, baseT],
	baseT, extendedT any,
	getRefFunc func(*extendedT) *baseT,
	baseOption generic.OptionFunc[baseT],
	extOptionsPtr optionsReference[extOptions, extOption, extendedT],
	extOptions optionSlice[extOption, extendedT],
	extOption generic.OptionFunc[extendedT],
](options extOptionsPtr, flagSet *flag.FlagSet, getRefFn getRefFunc,
) {
	var base constructor
	binder(&base).BindFlags(flagSet)
	*options = append(*options,
		func(extended *extendedT) error {
			subset, err := base.make()
			if err != nil {
				return err
			}
			*(getRefFn(extended)) = subset
			return nil
		},
	)
}
