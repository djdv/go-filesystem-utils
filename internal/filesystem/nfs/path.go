package nfs

const (
	posixSeparator = '/'
	dosSeparator   = '\\'
)

func targetIsInvalid(path string) bool {
	if len(path) >= 1 && path[0] == posixSeparator {
		return true
	}
	if isVolume(path) {
		return true
	}
	return false
}

// isVolume is a modification of [filepath.VolumeName]
// (and its callees) for non-Windows GOOS systems.
func isVolume(path string) bool {
	const (
		driveLetterSize = 2
		uncSlashCount   = 2
		uncPrefix       = `\\.\UNC`
	)
	switch {
	case len(path) >= driveLetterSize && path[1] == ':':
		return true
	case pathHasPrefixFold(path, uncPrefix):
		return true
	case len(path) >= uncSlashCount && isSlash(path[1]):
		return true
	default:
		return false
	}
}

func isSlash(c uint8) bool {
	return c == dosSeparator || c == posixSeparator
}

func pathHasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if isSlash(prefix[i]) {
			if !isSlash(s[i]) {
				return false
			}
		} else if toUpper(prefix[i]) != toUpper(s[i]) {
			return false
		}
	}
	if len(s) > len(prefix) && !isSlash(s[len(prefix)]) {
		return false
	}
	return true
}

func toUpper(c byte) byte {
	if 'a' <= c && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}
