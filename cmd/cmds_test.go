package fscmds_test

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

var root = &cmds.Command{
	Options: fscmds.RootOptions(),
	Helptext: cmds.HelpText{
		Tagline: "File system service ⚠[TEST]⚠ utility.",
	},
}

func TestRequestsStandard(t *testing.T) {
	var (
		ctx, cancel  = context.WithCancel(context.Background())
		request, err = cmds.NewRequest(ctx, nil, nil, nil, nil, root)
		callMethod   = func(method interface{},
			goArguments []reflect.Value) (got reflect.Value, provided bool, err error) {
			returnVector := reflect.ValueOf(method).Call(goArguments)
			got = returnVector[0]
			provided = returnVector[1].Bool()
			maybeErr := returnVector[2].Interface()
			if maybeErr != nil {
				var isErr bool
				if err, isErr = maybeErr.(error); !isErr {
					err = fmt.Errorf("return value is wrong type:\n\twanted:%T\n\tgot:%t",
						err, maybeErr)
				}
			}
			return
		}
		checkNotProvided = func(t *testing.T, typeName string, provided bool, got reflect.Value) {
			t.Helper()
			if provided {
				t.Fatalf("%s returned for bad request:\n\tProvided? %t\n\tValue: %#v",
					typeName, provided, got)
			}
		}
		checkProvided = func(t *testing.T, typeName string, provided bool) {
			t.Helper()
			if !provided {
				t.Fatalf("%s option was provided but not detected in request", typeName)
			}
		}
		checkSame = func(t *testing.T, typeName string, got reflect.Value, want interface{}) {
			t.Helper()
			if got.Interface().(interface{}) != want {
				t.Logf("%T != %T\n", got, want)
				t.Fatalf("%s option provided did not match input:\n\twant:%s\n\tgot:%s",
					typeName,
					want, got)
			}
		}
	)
	defer cancel()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Expected", func(t *testing.T) {
		var (
			parameters = fscmds.CmdsParameterSet{
				Name:        "test-argument",
				Environment: "TEST_ENV",
			}
			goArguments = []reflect.Value{
				reflect.ValueOf(request),
				reflect.ValueOf(parameters),
			}

			// shorthand:
			testParam = parameters.Name
			testEnv   = parameters.Environment
		)
		for _, test := range []struct {
			typeName  string
			value     interface{}
			getMethod interface{}
		}{
			{
				"string",
				"test string value",
				fscmds.GetStringArgument,
			},
			{
				"bool",
				true,
				fscmds.GetBoolArgument,
			},
		} {
			var (
				typeName = test.typeName
				getOpt   = test.getMethod
				optValue = test.value
			)
			t.Run(typeName, func(t *testing.T) {
				t.Run("Options", func(t *testing.T) {
					got, provided, err := callMethod(getOpt, goArguments)
					if err != nil {
						t.Fatal(err)
					}
					checkNotProvided(t, typeName, provided, got)

					request.SetOption(testParam, optValue)
					got, provided, err = callMethod(getOpt, goArguments)
					delete(request.Options, testParam)
					if err != nil {
						t.Fatal(err)
					}
					checkProvided(t, typeName, provided)
					checkSame(t, typeName, got, optValue)
				})
				t.Run("Environment", func(t *testing.T) {
					got, provided, err := callMethod(getOpt, goArguments)
					if err != nil {
						t.Fatal(err)
					}
					checkNotProvided(t, typeName, provided, got)

					osErr := os.Setenv(testEnv, fmt.Sprintf("%v", optValue))
					if osErr != nil {
						t.Fatalf("failed to set environment %q: %s", testEnv, osErr)
					}
					got, provided, err = callMethod(getOpt, goArguments)
					if osErr := os.Unsetenv(testEnv); osErr != nil {
						t.Fatalf("failed to unset environment %q: %s", testEnv, osErr)
					}
					if err != nil {
						t.Fatal(err)
					}

					checkProvided(t, typeName, provided)
					checkSame(t, typeName, got, optValue)
				})
			})
		}
	})

	t.Run("Unexpected", func(t *testing.T) {
		var (
			badParameters = fscmds.CmdsParameterSet{
				Name:        "bad-argument",
				Environment: "BAD_ENV",
			}
			badValue    = struct{}{}
			goArguments = []reflect.Value{
				reflect.ValueOf(request),
				reflect.ValueOf(badParameters),
			}
		)
		for _, test := range []struct {
			typeName  string
			getMethod interface{}
		}{
			{
				"string",
				fscmds.GetStringArgument,
			},
			{
				"bool",
				fscmds.GetBoolArgument,
			},
			{
				"Duration",
				fscmds.GetDurationArgument,
			},
		} {
			var (
				typeName = test.typeName
				getOpt   = test.getMethod

				// shorthand:
				badParameter = badParameters.Name
				badEnv       = badParameters.Environment
			)
			t.Run(typeName, func(t *testing.T) {
				t.Run("Options", func(t *testing.T) {
					got, provided, err := callMethod(getOpt, goArguments)
					if err != nil {
						t.Fatal(err)
					}
					checkNotProvided(t, typeName, provided, got)

					request.SetOption(badParameter, badValue)
					_, provided, err = callMethod(getOpt, goArguments)
					if err == nil {
						t.Fatalf("expected type error for bad request\n\ttype: %s\n\tvalue: %#v",
							typeName, badValue)
					}
					delete(request.Options, badParameter)
					checkProvided(t, typeName, provided)
				})
				t.Run("Environment", func(t *testing.T) {
					got, provided, err := callMethod(getOpt, goArguments)
					if err != nil {
						t.Fatal(err)
					}
					checkNotProvided(t, typeName, provided, got)

					if typeName == "string" {
						return // skip testing env type for strings
					}

					osErr := os.Setenv(badEnv, "Intentionally invalid test value for type: "+
						typeName)
					if osErr != nil {
						t.Fatalf("failed to set environment %q: %s", badEnv, osErr)
					}
					_, provided, err = callMethod(getOpt, goArguments)
					if osErr := os.Unsetenv(badEnv); osErr != nil {
						t.Fatalf("failed to unset environment %q: %s", badEnv, osErr)
					}
					if err == nil {
						t.Fatalf("expected type error for bad request\n\ttype: %s\n\tvalue: %#v",
							typeName, badValue)
					}
					checkProvided(t, typeName, provided)
				})
			})
		}
	})
}

func TestRequestMultiaddr(t *testing.T) {
	t.Run("Unexpected", testRequestMultiaddrUnexpected)
	t.Run("Expected", testRequestMultiaddrExpected)
}

func testRequestMultiaddrUnexpected(t *testing.T) {
	const badMaddr = "\\invalid\\"
	var (
		badParameters = fscmds.CmdsParameterSet{
			Name:        "bad-maddr-option-type",
			Environment: "BAD_MADDR",
		}
		ctx, cancel   = context.WithCancel(context.Background())
		badValue      = struct{}{}
		checkProvided = func(t *testing.T, got multiaddr.Multiaddr, provided bool, err error) {
			if err == nil {
				t.Fatalf("multiaddr returned for bad request:\n\tProvided? %t\n\tValue: %#v",
					provided, got,
				)
			}
			if !provided {
				t.Fatalf("option was provided but not detected in request")
			}
		}
	)
	defer cancel()
	request, err := cmds.NewRequest(ctx, nil,
		cmds.OptMap{
			badParameters.Name: badValue,
		},
		nil, nil, root)
	if err != nil {
		t.Fatal(err)
	}

	var ( // shorthand:
		badParameter = badParameters.Name
		badEnv       = badParameters.Environment
	)
	t.Run("Environment Variable", func(t *testing.T) {
		if err := os.Setenv(badEnv, badMaddr); err != nil {
			t.Fatalf("failed to set environment %q to %v\n\t%s",
				badEnv, badMaddr, err)
		}
		got, provided, err := fscmds.GetMultiaddrArgument(request, badParameters)
		if osErr := os.Unsetenv(badEnv); osErr != nil {
			t.Fatalf("failed to unset environment %q: %s",
				badEnv, osErr)
		}
		checkProvided(t, got, provided, err)
	})
	t.Run("Options", func(t *testing.T) {
		request.SetOption(badParameter, badValue)
		got, provided, err := fscmds.GetMultiaddrArgument(request, badParameters)
		delete(request.Options, badParameter)
		checkProvided(t, got, provided, err)
	})
}

func testRequestMultiaddrExpected(t *testing.T) {
	var (
		ctx, cancel   = context.WithCancel(context.Background())
		checkProvided = func(t *testing.T, request *cmds.Request, want multiaddr.Multiaddr,
			parameters fscmds.CmdsParameterSet) {
			got, provided, err := fscmds.GetMultiaddrArgument(request, parameters)
			if err != nil {
				t.Fatal(err)
			}
			if !provided {
				t.Fatalf("multiaddr %s not found in request", want.String())
			}
			if !got.Equal(want) {
				t.Fatalf("multiaddr request input and output do not match:\n\t%v\n\t\t!=\n\t %v",
					want,
					got)
			}
		}
	)
	defer cancel()
	for _, test := range []struct {
		parameters fscmds.CmdsParameterSet
		value      string
	}{
		{
			fscmds.CmdsParameterSet{
				Name:        "test-maddr1",
				Environment: "TEST_MADDR1",
			},
			"/ip4/127.0.0.1/tcp/80",
		},
		{
			fscmds.CmdsParameterSet{
				Name:        "test-maddr2",
				Environment: "TEST_MADDR2",
			},
			"/dns4/localhost",
		},
	} {
		var (
			testParameters = test.parameters
			testValue      = test.value

			// shorthand:
			testParameter = testParameters.Name
			testEnv       = testParameters.Environment
		)
		t.Run(testValue, func(t *testing.T) {
			want, err := multiaddr.NewMultiaddr(testValue)
			if err != nil {
				t.Fatal(err)
			}
			request, err := cmds.NewRequest(ctx, nil, nil, nil, nil, root)
			if err != nil {
				t.Fatal(err)
			}

			t.Run("Environment Variable", func(t *testing.T) {
				if err := os.Setenv(testEnv, testValue); err != nil {
					t.Fatalf("failed to set environment %q to %v\n\t%s",
						testEnv, testValue, err)
				}
				checkProvided(t, request, want, testParameters)
				if err := os.Unsetenv(testEnv); err != nil {
					t.Fatalf("failed to unset environment %q: %s", testEnv, err)
				}
			})
			t.Run("Options", func(t *testing.T) {
				request.SetOption(testParameter, testValue)
				checkProvided(t, request, want, testParameters)
				delete(request.Options, testParameter)
			})
		})
	}
}
