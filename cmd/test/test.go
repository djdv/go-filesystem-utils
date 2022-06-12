package testcmd

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	setp "github.com/djdv/go-filesystem-utils/internal/cmds/setting"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/argument"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/option"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/request"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"golang.org/x/exp/constraints"
)

type Settings struct {
	Port       int
	Something  customID
	Something2 fsID
	Time       time.Duration
	An         another
}

type (
	customID uint
	fsID     uint
)

//go:generate stringer --type=customID
const (
	thingStart customID = iota
	IPFS
	IPNS
	MFS
	thingEnd
)

//go:generate stringer --type=fsID
const (
	fsidStart fsID = iota
	fuse
	p9
	fsidEnd
)

//go:generate stringer --type=another
type another uint

const (
	anotherStart another = iota
	another1
	another2
	anotherEnd
)

type Enum interface {
	constraints.Integer
	fmt.Stringer
}

func enumWrapper[e Enum](start, end e) func(string) (e, error) {
	return func(s string) (e, error) {
		return ParseEnum(start, end, s)
	}
}

func ParseEnum[e Enum](start, end e, s string) (e, error) {
	normalized := strings.ToLower(s)
	for i := start + 1; i != end; i++ {
		var (
			thing  e = i
			strVal   = strings.ToLower(thing.String())
		)
		if normalized == strVal {
			return thing, nil
		}
	}
	return start, fmt.Errorf("invalid Enum: \"%s\"", s)
}

func (*Settings) Parameters(ctx context.Context) parameter.Parameters {
	partials := []setp.CmdsParameter{
		{
			OptionName:    "not-port",
			HelpText:      "whatever",
			OptionAliases: []string{"p"},
		},
		{HelpText: "something else"},
		{HelpText: "something elser"},
		{HelpText: "something elserer"},
		{HelpText: "something elsererer"},
	}
	return setp.MustMakeParameters[*Settings](ctx, partials)
}

func Command() *cmds.Command {
	opts, _ := option.MakeOptions[*Settings](
		option.WithConstructor(
			option.NewOptionConstructor(time.ParseDuration),
		),
		option.WithConstructor(
			option.NewOptionConstructor(enumWrapper(thingStart, thingEnd)),
		),
		option.WithConstructor(
			option.NewOptionConstructor(enumWrapper(fsidStart, fsidEnd)),
		),
		option.WithConstructor(
			option.NewOptionConstructor(enumWrapper(anotherStart, anotherEnd)),
		),
	)
	return &cmds.Command{
		Helptext: cmds.HelpText{
			Tagline: "test utility",
		},
		Options: opts,
		Run: func(r *cmds.Request, re cmds.ResponseEmitter, e cmds.Environment) error {
			var (
				ctx     = context.Background()
				parsers = []argument.Parser{
					argument.NewParser(time.ParseDuration),
					argument.NewParser(enumWrapper(thingStart, thingEnd)),
					argument.NewParser(enumWrapper(fsidStart, fsidEnd)),
					argument.NewParser(enumWrapper(anotherStart, anotherEnd)),
				}
				sources = []argument.SetFunc{request.ValueSource(r)}
			)
			settings, err := argument.Parse[*Settings](ctx, sources, parsers...)
			if err != nil {
				return err
			}

			log.Println("Settings:", settings)
			log.Printf("Settings: %#v", settings)

			return nil
		},
	}
}
