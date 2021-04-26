package service_test

import (
	"context"
	"testing"

	"github.com/djdv/go-filesystem-utils/cmd/service"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func TestExecutor(t *testing.T) {
	t.Run("nil inputs", func(t *testing.T) {
		defer func() { recover() }() // nil args are expected to panic.
		_, err := service.MakeExecutor(nil, nil)
		if err == nil {
			t.Fatal("executor was returned with nil constructor arguments")
		}
	})

	ctx := context.Background()
	request, err := cmds.NewRequest(ctx, []string{service.Name}, nil,
		nil, nil, testRoot)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Regular request", func(t *testing.T) {
		fsEnv, err := service.MakeEnvironment(ctx, request)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = service.MakeExecutor(request, fsEnv); err != nil {
			t.Fatal(err)
		}
	})

	// TODO: type check return from MakeExecutor
	// NoLocal requests should be of type *http.clients
	// If not, fail.
}
