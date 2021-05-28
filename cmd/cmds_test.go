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

func genericGetArgument(method interface{},
	goArguments []reflect.Value) (got reflect.Value, provided bool, err error) {
	returnVector := reflect.ValueOf(method).Call(goArguments)
	got = returnVector[0]
	provided = returnVector[1].Bool()
	maybeErr := returnVector[2].Interface()
	if maybeErr != nil {
		var isErr bool
		err, isErr = maybeErr.(error)
		if !isErr {
			err = fmt.Errorf("return value is wrong type:\n\twanted:%T\n\tgot:%t",
				err, maybeErr)
		}
	}
	return
}

type genericParameterArgumentPair struct {
	// The parameter's keys
	fscmds.CmdsParameterSet
	// And the value for its methods.
	arguments []reflect.Value
}

func TestRequestsStandard(t *testing.T) {
	var (
		ctx, cancel           = context.WithCancel(context.Background())
		request, requestError = cmds.NewRequest(ctx, nil, nil, nil, nil, root)
	)
	defer cancel()
	if requestError != nil {
		t.Fatal(requestError)
	}

	var (
		expectedParameters = fscmds.CmdsParameterSet{
			Name:        "test-argument",
			Environment: "TEST_ENV",
		}
		expectedSet = genericParameterArgumentPair{
			CmdsParameterSet: expectedParameters,
			arguments: []reflect.Value{
				reflect.ValueOf(request),
				reflect.ValueOf(expectedParameters),
			},
		}
		unexpectedParameters = fscmds.CmdsParameterSet{
			Name:        "bad-argument",
			Environment: "BAD_ENV",
		}
		unexpectedSet = genericParameterArgumentPair{
			CmdsParameterSet: unexpectedParameters,
			arguments: []reflect.Value{
				reflect.ValueOf(request),
				reflect.ValueOf(unexpectedParameters),
			},
		}
	)
	t.Run("Erroneous input", func(t *testing.T) { erroneousArguments(t, request, unexpectedSet) })
	t.Run("Expected input", func(t *testing.T) { expectedArguments(t, request, expectedSet) })
}

func expectedArguments(t *testing.T, request *cmds.Request, set genericParameterArgumentPair) {
	t.Helper()
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
				got, provided := genericCheckArgument(t,
					getOpt, set.arguments)
				genericArgWasNotProvided(t,
					typeName, provided, got)

				request.SetOption(set.Name, optValue)
				got, provided = genericCheckArgument(t,
					getOpt, set.arguments)
				delete(request.Options, set.Name)

				genericArgWasProvided(t,
					typeName, provided)
				genericArgIsSame(t,
					typeName, got, optValue)
			})
			t.Run("Environment", func(t *testing.T) {
				got, provided := genericCheckArgument(t,
					getOpt, set.arguments)
				genericArgWasNotProvided(t,
					typeName, provided, got)

				osErr := os.Setenv(set.Environment, fmt.Sprintf("%v", optValue))
				if osErr != nil {
					t.Fatalf("failed to set environment %q: %s",
						set.Environment, osErr)
				}
				got, provided = genericCheckArgument(t,
					getOpt, set.arguments)
				if osErr := os.Unsetenv(set.Environment); osErr != nil {
					t.Fatalf("failed to unset environment %q: %s",
						set.Environment, osErr)
				}

				genericArgWasProvided(t,
					typeName, provided)
				genericArgIsSame(t,
					typeName, got, optValue)
			})
		})
	}
}

func erroneousArguments(t *testing.T, request *cmds.Request, set genericParameterArgumentPair) {
	t.Helper()
	var badValue = struct{}{}
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
		)
		t.Run(typeName, func(t *testing.T) {
			t.Run("Options", func(t *testing.T) {
				got, provided := genericCheckArgument(t,
					getOpt, set.arguments)
				genericArgWasNotProvided(t,
					typeName, provided, got)

				request.SetOption(set.Name, badValue)
				_, provided, err := genericGetArgument(getOpt, set.arguments)
				if err == nil {
					t.Fatalf("expected type error for bad request\n\ttype: %s\n\tvalue: %#v",
						typeName, badValue)
				}
				delete(request.Options, set.Name)

				genericArgWasProvided(t,
					typeName, provided)
			})
			t.Run("Environment", func(t *testing.T) {
				got, provided := genericCheckArgument(t,
					getOpt, set.arguments)
				genericArgWasNotProvided(t,
					typeName, provided, got)

				if typeName == "string" {
					t.Skip("don't test string type against env var 0 value")
				}

				osErr := os.Setenv(set.Environment,
					"Intentionally invalid test value for type: "+
						typeName)
				if osErr != nil {
					t.Fatalf("failed to set environment %q: %s",
						set.Environment, osErr)
				}
				_, provided, err := genericGetArgument(getOpt, set.arguments)
				if osErr := os.Unsetenv(set.Environment); osErr != nil {
					t.Fatalf("failed to unset environment %q: %s",
						set.Environment, osErr)
				}
				if err == nil {
					t.Fatalf("expected type error for bad request\n\ttype: %s\n\tvalue: %#v",
						typeName, badValue)
				}

				genericArgWasProvided(t,
					typeName, provided)
			})
		})
	}
}

func genericCheckArgument(t *testing.T, method interface{},
	goArguments []reflect.Value) (got reflect.Value, provided bool) {
	t.Helper()
	var err error
	got, provided, err = genericGetArgument(method, goArguments)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func genericArgIsSame(t *testing.T, typeName string, got reflect.Value, want interface{}) {
	t.Helper()
	if got.Interface().(interface{}) != want {
		t.Logf("%T != %T\n", got, want)
		t.Fatalf("%s option provided did not match input:\n\twant:%s\n\tgot:%s",
			typeName,
			want, got)
	}
}

func genericArgWasProvided(t *testing.T, typeName string, provided bool) {
	t.Helper()
	if !provided {
		t.Fatalf("%s option was provided but not detected in request",
			typeName)
	}
}

func genericArgWasNotProvided(t *testing.T, typeName string, provided bool, got reflect.Value) {
	t.Helper()
	if provided {
		t.Fatalf("%s returned for bad request:\n\tProvided? %t\n\tValue: %#v",
			typeName, provided, got)
	}
}

func TestRequestMultiaddr(t *testing.T) {
	t.Run("Erroneous input", testRequestMultiaddrUnexpected)
	t.Run("Expected input", testRequestMultiaddrExpected)
}

func testRequestMultiaddrUnexpected(t *testing.T) {
	const badMaddr = "\\invalid\\"
	var (
		badValue      = struct{}{}
		badParameters = fscmds.CmdsParameterSet{
			Name:        "bad-maddr-option-type",
			Environment: "BAD_MADDR",
		}
		checkProvided = func(t *testing.T, got multiaddr.Multiaddr, provided bool, err error) {
			t.Helper()
			if err == nil {
				t.Fatalf("multiaddr returned for bad request:\n\tProvided? %t\n\tValue: %#v",
					provided, got,
				)
			}
			if !provided {
				t.Fatalf("option was provided but not detected in request")
			}
		}
		ctx, cancel = context.WithCancel(context.Background())
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

	t.Run("Options", func(t *testing.T) {
		request.SetOption(badParameters.Name, badValue)
		got, provided, err := fscmds.GetMultiaddrArgument(request, badParameters)
		delete(request.Options, badParameters.Name)
		checkProvided(t, got, provided, err)
	})
	t.Run("Environment", func(t *testing.T) {
		if err := os.Setenv(badParameters.Environment, badMaddr); err != nil {
			t.Fatalf("failed to set environment %q to %v\n\t%s",
				badParameters.Environment, badMaddr, err)
		}
		got, provided, err := fscmds.GetMultiaddrArgument(request, badParameters)
		if osErr := os.Unsetenv(badParameters.Environment); osErr != nil {
			t.Fatalf("failed to unset environment %q: %s",
				badParameters.Environment, osErr)
		}
		checkProvided(t, got, provided, err)
	})
}

func testRequestMultiaddrExpected(t *testing.T) {
	t.Helper()
	var (
		ctx, cancel   = context.WithCancel(context.Background())
		checkProvided = func(t *testing.T, request *cmds.Request, want multiaddr.Multiaddr,
			parameters fscmds.CmdsParameterSet) {
			t.Helper()
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
				if err := os.Setenv(testParameters.Environment, testValue); err != nil {
					t.Fatalf("failed to set environment %q to %v\n\t%s",
						testParameters.Environment, testValue, err)
				}

				checkProvided(t,
					request, want, testParameters)

				if err := os.Unsetenv(testParameters.Environment); err != nil {
					t.Fatalf("failed to unset environment %q: %s",
						testParameters.Environment, err)
				}
			})
			t.Run("Options", func(t *testing.T) {
				request.SetOption(testParameters.Name, testValue)

				checkProvided(t,
					request, want, testParameters)

				delete(request.Options, testParameters.Name)
			})
		})
	}
}
