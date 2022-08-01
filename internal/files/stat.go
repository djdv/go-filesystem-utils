package files

import (
	"sync/atomic"

	"github.com/hugelgupf/p9/p9"
)

const (
	// TODO: [lint] these aren't really necessary anymore.
	_ p9.Dev = iota
	rootDev
	stopperDev
)

// If the optional parent pather was provided, use it
// otherwise, make a new one and use that.
func setupOrUsePather(path *uint64, patherPtr **atomic.Uint64) {
	pather := *patherPtr
	if pather == nil {
		pather = new(atomic.Uint64)
		*patherPtr = pather
	}
	*path = pather.Add(1)
}

func fillAttrs(req p9.AttrMask, attr *p9.Attr) (p9.AttrMask, *p9.Attr) {
	var (
		rAttr  p9.Attr
		filled p9.AttrMask
	)
	if req.Empty() {
		return filled, &rAttr
	}

	if req.Mode {
		rAttr.Mode, filled.Mode = attr.Mode, true
	}
	if req.UID {
		rAttr.UID, filled.UID = attr.UID, true
	}
	if req.GID {
		rAttr.GID, filled.GID = attr.GID, true
	}
	if req.RDev {
		rAttr.RDev, filled.RDev = attr.RDev, true
	}
	if req.Size {
		rAttr.Size, filled.Size = attr.Size, true
	}

	return filled, &rAttr
}
