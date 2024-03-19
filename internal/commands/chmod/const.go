package chmod

import (
	"io/fs"
	"os"
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
)
