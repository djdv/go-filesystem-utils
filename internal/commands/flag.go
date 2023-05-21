package commands

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/hugelgupf/p9/p9"
	giconfig "github.com/ipfs/kubo/config"
	"github.com/multiformats/go-multiaddr"
)

type (
	commonSettings struct {
		command.HelpArg
		verbose bool
	}
	daemonDecay struct {
		exitInterval time.Duration
	}
	nineIDs struct {
		uid p9.UID
		gid p9.GID
	}

	// flagDefaultText is a map of flag names and the text
	// for their default values.
	// This may be provided explicitly when the
	// [fmt.Stringer] output of a flag's default value,
	// isn't as appropriate for a command's "help" text.
	// E.g. you may want to display "none" instead of "0",
	// or a literal `$ENV/filename` rather than
	// `*fully-resolved-path*/filename`, etc.
	flagDefaultText map[string]string

	// lazyFlag may be implemented, to allow
	// flags to initialize default values at command
	// invocation time, rather than at process
	// initialization time.
	// This helps reduce process startup time, by delaying
	// expansion of flags that perform slow operations
	// (disk/net I/O, etc.), for values
	// that might not even be needed if the caller has
	// set it explicitly, or for values that belong
	// to another command than the one being invoked.
	lazyFlag[T any] interface{ get() (T, error) }

	// lazyInitializer may be implemented by a guest or host
	// settings structure, typically to cascade
	// initialization of lazy flags.
	// Such as by type checking each field
	// and initializing its value if it is of type [lazyFlag].
	lazyInitializer interface {
		lazyInit() error
	}

	// defaultIPFSMaddr distinguishes
	// the default maddr value, from an arbitrary maddr value.
	// I.e. even if the underlying multiaddrs are the same
	// only the flag's default value should be of this type.
	// Implying the flag was not provided/set explicitly.
	//
	// It also implements the lazyFlag interface,
	// since it needs to perform I/O to find
	// a dynamic/system local value.
	defaultIPFSMaddr struct {
		multiaddr.Multiaddr
		flagName string
	}
)

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

	ipfsAPIFileName      = "api"
	ipfsConfigEnv        = giconfig.EnvDir
	ipfsConfigDefaultDir = giconfig.DefaultPathRoot
)

func setDefaultValueText(flagSet *flag.FlagSet, defaultText flagDefaultText) {
	flagSet.VisitAll(func(f *flag.Flag) {
		if text, ok := defaultText[f.Name]; ok {
			f.DefValue = text
		}
	})
}

func (di *defaultIPFSMaddr) get() (multiaddr.Multiaddr, error) {
	maddr := di.Multiaddr
	if maddr == nil {
		maddrs, err := getIPFSAPI()
		if err != nil {
			return nil, fmt.Errorf(
				"could not get default value for `-%s` flag: %w",
				di.flagName, err,
			)
		}
		maddr = maddrs[0]
		di.Multiaddr = maddr
	}
	return maddr, nil
}

func (set *commonSettings) BindFlags(flagSet *flag.FlagSet) {
	set.HelpArg.BindFlags(flagSet)
	flagSet.BoolVar(&set.verbose, "verbose",
		false, "enable log messages")
}

func parseID[id uint32 | p9.UID | p9.GID](arg string) (id, error) {
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

func getIPFSAPI() ([]multiaddr.Multiaddr, error) {
	location, err := getIPFSAPIPath()
	if err != nil {
		return nil, err
	}
	if !apiFileExists(location) {
		return nil, generic.ConstError("IPFS API file not found")
	}
	return parseIPFSAPI(location)
}

func getIPFSAPIPath() (string, error) {
	var target string
	if ipfsPath, set := os.LookupEnv(ipfsConfigEnv); set {
		target = filepath.Join(ipfsPath, ipfsAPIFileName)
	} else {
		target = filepath.Join(ipfsConfigDefaultDir, ipfsAPIFileName)
	}
	return expandHomeShorthand(target)
}

func expandHomeShorthand(name string) (string, error) {
	if !strings.HasPrefix(name, "~") {
		return name, nil
	}
	homeName, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeName, name[1:]), nil
}

func apiFileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func parseIPFSAPI(name string) ([]multiaddr.Multiaddr, error) {
	// NOTE: [upstream problem]
	// If the config file has multiple API maddrs defined,
	// only the first one will be contained in the API file.
	maddrString, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	maddr, err := multiaddr.NewMultiaddr(string(maddrString))
	if err != nil {
		return nil, err
	}
	return []multiaddr.Multiaddr{maddr}, nil
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
	switch op := []rune(clauseOp)[0]; op {
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
	switch who := []rune(clauseFragment)[0]; who {
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
			sb.WriteRune(',')
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
	sb.WriteRune('=')
	for _, r := range runes {
		sb.WriteRune(r)
	}
	if haveSpecial {
		sb.WriteRune(specSym)
	}
}
