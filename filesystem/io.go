package filesystem

type IOFlags uint

const (
	// TODO: (re)consider how these should be defined and what we want/need
	// for now we mimick SUSv7's <fcntl.h>
	IOReadOnly IOFlags = 1 << iota
	IOReadWrite
	IOWriteOnly
	/* consider if we want to support
	   append (writes)
	   create (conditional)
	   excl(usive create)
	   trunc(ate file)
	*/
)
