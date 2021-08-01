package fscmds_test

import (
	"context"
	"errors"
	"testing"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func TestUtilities(t *testing.T) {
	t.Run("find server", func(t *testing.T) {
		srv, err := fscmds.FindLocalServer()
		if err == nil {
			t.Error("active server was found when not expected: ", srv)
		}
		expectedErr := fscmds.ErrServiceNotFound
		if !errors.Is(err, expectedErr) {
			t.Errorf("unexpected error"+
				"\n\twanted: %v"+
				"\n\tgot:%v",
				expectedErr, err)
		}
	})
}

func TestParameters(t *testing.T) {
	var (
		expectedMaddrStrings = []string{
			"/dns4/localhost",
			"/ip4/127.0.0.1/tcp/80",
		}
		expectedStopInterval = time.Second * 3

		ctx, cancel = context.WithCancel(context.Background())
		root        = &cmds.Command{
			Options: fscmds.RootOptions(),
			Helptext: cmds.HelpText{
				Tagline: "File system service ⚠[TEST]⚠ utility.",
			},
		}
		request, requestError = cmds.NewRequest(ctx, nil,
			cmds.OptMap{
				fscmds.ServiceMaddr().CommandLine(): expectedMaddrStrings,
				fscmds.StopAfter().CommandLine():    expectedStopInterval.String(),
			},
			nil, nil, root)
	)
	defer cancel()
	if requestError != nil {
		t.Fatal(requestError)
	}

	// TODO: t.run these scopes
	{ // Round 1 - full set
		var (
			settings        = new(fscmds.Settings)
			unsetArgs, errs = parameters.ParseSettings(ctx, settings,
				parameters.SettingsFromCmds(request),
				parameters.SettingsFromEnvironment(),
			)
			unset, err = parameters.AccumulateArgs(ctx, unsetArgs, errs)
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(unset) != 0 {
			t.Fatal("TODO message about args not being set")
		}

		for i, serviceMaddr := range settings.ServiceMaddrs {
			if serviceMaddr.String() != expectedMaddrStrings[i] {
				t.Fatal("TODO something about maddrs not matching")
			}
		}

		if settings.AutoExit != expectedStopInterval {
			t.Fatal("TODO something about time being wrong")
		}
	}

	{ // Round 2 - partial set
		delete(request.Options, fscmds.StopAfter().CommandLine())
		var (
			settings        = new(fscmds.Settings)
			unsetArgs, errs = parameters.ParseSettings(ctx, settings,
				parameters.SettingsFromCmds(request),
				parameters.SettingsFromEnvironment(),
			)
			unset, err = parameters.AccumulateArgs(ctx, unsetArgs, errs)

			expectedUnsetParam = fscmds.StopAfter()
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(unset) < 1 {
			t.Fatal("TODO message about args not being unset")
		}

		for _, unsetArg := range unset {
			if unsetArg.Parameter == expectedUnsetParam {
				return // Expected behaviour.
			}
		}
		t.Fatal("TODO message about unset args not being correct")
	}
}
