package cgofuse

import (
	"strconv"
	"strings"
)

type (
	errNo           = int
	fileDescriptor  = uint64
	filePermissions = uint32
	id              = uint32
	uid             = id
	gid             = id
	fuseContext     struct {
		uid
		gid
		// NOTE: PID omitted as not used.
	}
)

const (
	idOptionBody    = `id=`
	optionDelimiter = ','
	delimiterSize   = len(string(optionDelimiter))
)

func idOptionPre(id uint32) (string, int) {
	var (
		idStr = strconv.Itoa(int(id))
		size  = 1 + len(idOptionBody) + len(idStr)
	)
	return idStr, size
}

func idOption(option *strings.Builder, id string, leader rune) {
	option.WriteRune(leader)
	option.WriteString(idOptionBody)
	option.WriteString(id)
}
