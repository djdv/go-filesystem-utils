package service_test

import (
	"context"
	"testing"

	"github.com/djdv/go-filesystem-utils/cmd/service"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func TestEnvironment(t *testing.T) {
	ctx := context.Background()
	request, err := cmds.NewRequest(ctx, nil, nil, nil, nil, testRoot)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.MakeEnvironment(ctx, request); err != nil {
		t.Fatal(err)
	}
}
