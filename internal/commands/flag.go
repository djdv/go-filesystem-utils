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
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
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
	lazyFlag[T any]  interface{ get() (T, error) }
	defaultIPFSMaddr struct{ multiaddr.Multiaddr }
)

// TODO: move this
func WithIPFS(maddr multiaddr.Multiaddr) MountOption {
	return func(s *mountSettings) error { s.ipfs.nodeMaddr = maddr; return nil }
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

func (di defaultIPFSMaddr) String() string {
	maddr, err := di.get()
	if err != nil {
		return "no IPFS API file found (must provide this argument)"
	}
	return maddr.String()
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

func parseFSID(fsid string) (filesystem.ID, error) {
	normalizedID := filesystem.ID(strings.ToLower(fsid))
	for _, id := range p9fs.FileSystems() {
		if normalizedID == id {
			return id, nil
		}
	}
	err := fmt.Errorf(`unexpected file system id: "%s"`, fsid)
	return filesystem.ID(""), err
}

func parseHost(host string) (filesystem.Host, error) {
	normalizedHost := filesystem.Host(strings.ToLower(host))
	for _, api := range p9fs.Hosts() {
		if normalizedHost == api {
			return api, nil
		}
	}
	err := fmt.Errorf(`unexpected file system host: "%s"`, host)
	return filesystem.Host(""), err
}

// TODO: move these to ipfs.go?
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
	const apiFile = "api"
	var target string
	if ipfsPath, set := os.LookupEnv(giconfig.EnvDir); set {
		target = filepath.Join(ipfsPath, apiFile)
	} else {
		target = filepath.Join(giconfig.DefaultPathRoot, apiFile)
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
