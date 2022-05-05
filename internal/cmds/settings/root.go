package settings

import (
	"context"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type Root struct{}

func (*Root) Parameters(ctx context.Context) parameters.Parameters { return nil }
