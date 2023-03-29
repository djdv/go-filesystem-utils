package commands

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	helpOnly struct {
		command.HelpArg
	}
	commonSettings struct {
		helpOnly
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

	// defaultIPFSMaddr distinguishes
	// the default maddr value, from an arbitrary maddr value.
	// I.e. even if the underlying multiaddrs are the same
	// only the flag's default value should be of this type.
	// Implying the flag was not provided/set explicitly.
	//
	// It also implements the lazyFlag interface,
	// since it needs to perform I/O to find
	// a dynamic/system local value.
	defaultIPFSMaddr struct{ multiaddr.Multiaddr }
)

const (
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
			return nil, err
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

func parseID[id p9.UID | p9.GID](arg string) (id, error) {
	const idSize = 32
	num, err := strconv.ParseUint(arg, 0, idSize)
	if err != nil {
		return 0, err
	}
	return id(num), nil
}

func getIPFSAPI() ([]multiaddr.Multiaddr, error) {
	location, err := getIPFSAPIPath()
	if err != nil {
		return nil, err
	}
	if !apiFileExists(location) {
		return nil, errors.New("IPFS API file not found") // TODO: proper error value
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

// parsePOSIXPermissions accepts a SUSv4;BSi7 `chmod` mode operand.
func parsePOSIXPermissions(clauses string) (fs.FileMode, error) {
	const (
		base = 8
		bits = int(unsafe.Sizeof(fs.FileMode(0))) * base
	)
	if mode, err := strconv.ParseUint(clauses, base, bits); err == nil {
		return translateOctalPermissions(mode), nil
	}
	const (
		whoRe         = "([ugoa]*)"
		matchWho      = 1
		clauseRe      = "([-+=]+)"
		matchClause   = 2
		matchMin      = matchClause
		permRe        = "([rwxugo]*)"
		matchPerm     = 3
		historicRe    = "((?:[-+=]?[rwxugo]{1})*)"
		matchHistoric = 4
		fullRe        = "^" + whoRe + clauseRe + permRe + historicRe + "$"
		matchMax      = matchHistoric + 1
	)
	var (
		operations   = strings.Split(clauses, ",")
		permissionRe = regexp.MustCompile(fullRe)
		mode         fs.FileMode
	)
	for _, operation := range operations {
		matches := permissionRe.FindStringSubmatch(operation)
		if len(matches) < matchMin {
			return 0, fmt.Errorf(`%w: "%s" is neither an octal or symbolic permission`,
				strconv.ErrSyntax, operation)
		}
		if who := matches[matchWho]; who == "" || who == "a" {
			matches[matchWho] = "ugo"
		}
		var permBits fs.FileMode
		if len(matches) >= matchPerm {
			permBits = parseSymbolicPermission(&mode, matches[matchWho], matches[matchPerm])
		}
		if err := evaluatePermissionsExpression(&mode,
			matches[matchWho], matches[matchClause], permBits,
		); err != nil {
			return 0, err
		}
		if len(matches) >= matchHistoric {
			if err := handleHistoricPermissions(&mode, matches[matchWho], matches[matchHistoric]); err != nil {
				return 0, err
			}
		}
	}
	return mode, nil
}

func parseSymbolicPermission(mode *fs.FileMode, who, perm string) (bits fs.FileMode) {
	const (
		execute = 1 << iota
		write
		read

		otherShift             = 0
		groupShift             = 3
		userShift              = 6
		octalBits              = 3
		otherMask  fs.FileMode = (1 << octalBits) - 1
		groupMask  fs.FileMode = ((1 << octalBits) - 1) << groupShift
		userMask   fs.FileMode = ((1 << octalBits) - 1) << userShift
	)
	for _, r := range perm {
		switch r {
		case 'r':
			bits |= read
		case 'w':
			bits |= write
		case 'x':
			bits |= execute
		case 's':
			for _, r := range who {
				if r == 'u' {
					bits |= fs.ModeSetuid
				}
				if r == 'g' {
					bits |= fs.ModeSetgid
				}
			}
		case 't':
			bits |= fs.ModeSticky
		case 'X':
			if mode.IsDir() ||
				*mode&execute == 1 ||
				*mode&execute<<groupShift == 1 ||
				*mode&execute<<userShift == 1 {
				bits |= execute
			}
		case 'u':
			bits = *mode & userMask >> userShift
		case 'g':
			bits = *mode & groupMask >> groupShift
		case 'o':
			bits = *mode & otherMask
		}
	}
	return
}

func evaluatePermissionsExpression(mode *fs.FileMode, who string, ops string, perm fs.FileMode) error {
	for _, op := range ops {
		if err := evaluateOp(mode, who, op, perm); err != nil {
			return err
		}
	}
	return nil
}

func evaluateOp(mode *fs.FileMode, who string, op rune, perm fs.FileMode) error {
	const (
		otherShift             = 0
		groupShift             = 3
		userShift              = 6
		octalBits              = 3
		otherMask  fs.FileMode = (1 << octalBits) - 1
		groupMask  fs.FileMode = ((1 << octalBits) - 1) << groupShift
		userMask   fs.FileMode = ((1 << octalBits) - 1) << userShift
	)
	var operation func(shift uint, mask fs.FileMode)
	switch op {
	case '-':
		operation = func(shift uint, _ fs.FileMode) {
			*mode &^= perm << shift
		}
	case '+':
		operation = func(shift uint, _ fs.FileMode) {
			*mode |= perm << shift
		}
	case '=':
		operation = func(shift uint, mask fs.FileMode) {
			*mode = (*mode &^ mask) | perm<<shift
		}
	default:
		return fmt.Errorf(`"%c" is not a valid operator`, op)
	}
	for _, w := range who {
		switch w {
		case 'o':
			operation(otherShift, otherMask)
		case 'g':
			operation(groupShift, groupMask)
		case 'u':
			operation(userShift, userMask)
		default:
			return fmt.Errorf(`"%c" is not a valid \"who\" value`, w)
		}
	}
	return nil
}

// POSIX calls these "historical-practice forms".
func handleHistoricPermissions(mode *fs.FileMode, who, hist string) error {
	var op rune
	for _, opRune := range hist {
		if opRune == '-' || opRune == '+' || opRune == '=' {
			op = opRune
			continue
		}
		if err := evaluateOp(mode, who, op,
			parseSymbolicPermission(mode, who, string(opRune))); err != nil {
			return err
		}
		op = 0
	}
	return nil
}

func translateOctalPermissions(mode uint64) fs.FileMode {
	const (
		S_ISUID = 0o4000
		S_ISGID = 0o2000
		S_ISVTX = 0o1000
	)
	fsMode := fs.FileMode(mode) & fs.ModePerm
	for _, bit := range [...]struct {
		golang fs.FileMode
		posix  uint64
	}{
		{golang: fs.ModeSetuid, posix: S_ISUID},
		{golang: fs.ModeSetgid, posix: S_ISGID},
		{golang: fs.ModeSticky, posix: S_ISVTX},
	} {
		if mode&bit.posix != 0 {
			fsMode |= bit.golang
		}
	}
	return fsMode
}
