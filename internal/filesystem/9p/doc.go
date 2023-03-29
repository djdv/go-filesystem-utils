// Package p9 implements file systems for the
// Plan 9 File Protocol.
package p9

import "github.com/hugelgupf/p9/perrors"

// NOTE: [2023.01.02]
// The reference documentation and implementation
// do not specify which error number to use.
// If this value seems incorrect, request to change it.
const fidOpenedErr = perrors.EBUSY
