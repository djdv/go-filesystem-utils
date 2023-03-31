//go:build !go1.21

package errors

import "github.com/djdv/go-filesystem-utils/internal/generic"

const ErrUnsupported = generic.ConstError("unsupported operation")
