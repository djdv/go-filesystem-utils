package service_test

import (
	"context"
	"testing"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func TestEnvironment(t *testing.T) {
	ctx := context.Background()
	request, err := cmds.NewRequest(ctx, nil, nil, nil, nil, testRoot)
	if err != nil {
		t.Fatal(err)
	}

	env, err := service.MakeEnvironment(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, envIsUsable := env.(fscmds.ServiceEnvironment); !envIsUsable {
		t.Fatalf("environment returned from make %T does not implement our environment interface", env)
	}
}
