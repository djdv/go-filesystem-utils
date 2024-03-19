package chmod

import (
	"io/fs"
	"strings"
)

// ToSymbolic renders `permissions` in the
// SUSv4;BSi7 "symbolic_mode" format..
func ToSymbolic(permissions fs.FileMode) string {
	// TODO: Extend this to support file type?
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
		writePermSymbols(&sb, permissions, pair.whoMask, pair.specMask, pair.whoSymbol, pair.specSymbol)
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
