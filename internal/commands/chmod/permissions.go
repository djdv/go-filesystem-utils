package chmod

import (
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

// ParsePermissions accepts a `chmod` "mode" parameter
// (as defined in SUSv4;BSi7), and returns the result of
// applying it to the `mode` value.
func ParsePermissions(mode fs.FileMode, clauses string) (fs.FileMode, error) {
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
		return parseOctal(mode, fs.FileMode(value)), nil
	}
	return evalPermissionClauses(
		mode,
		parseOctal(0, getUmask()),
		strings.Split(clauses, ","),
	)
}

func parseOctal(mode, operand fs.FileMode) fs.FileMode {
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
