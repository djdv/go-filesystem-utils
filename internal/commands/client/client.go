package client

import (
	"errors"
	"fmt"
	"io"
	golog "log"
	"os"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/commands/daemon"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	settings struct {
		maddr               multiaddr.Multiaddr
		exitInterval        time.Duration
		autoLaunch, verbose bool
	}
	Option  func(*settings) error
	Options []Option
)

const (
	errServiceConnection = generic.ConstError("could not connect to service")
	ErrCouldNotDial      = generic.ConstError("could not dial")
)

func (co *Options) GetClient() (*p9.Client, error) {
	var settings settings
	if err := generic.ApplyOptions(&settings, *co...); err != nil {
		return nil, err
	}
	maddr := settings.maddr
	if maddr == nil {
		maddr = daemon.DefaultAPIMaddr()
	}
	var (
		maddrs   = []multiaddr.Multiaddr{maddr}
		nineOpts = settings.nineOpts()
	)
	client, err := connect(maddrs, nineOpts...)
	if err == nil {
		return client, nil
	}
	if settings.autoLaunch &&
		errors.Is(err, ErrCouldNotDial) {
		return launchAndConnect(settings.exitInterval, nineOpts...)
	}
	return nil, err
}

func connect(maddrs []multiaddr.Multiaddr, options ...p9.ClientOpt) (*p9.Client, error) {
	conn, err := firstDialable(maddrs...)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %w",
			errServiceConnection, err,
		)
	}
	return newClient(conn, options...)
}

func firstDialable(maddrs ...multiaddr.Multiaddr) (manet.Conn, error) {
	for _, maddr := range maddrs {
		if conn, err := manet.Dial(maddr); err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf(
		"%w any of: %s",
		ErrCouldNotDial,
		formatMaddrs(maddrs),
	)
}

func formatMaddrs(maddrs []multiaddr.Multiaddr) string {
	if len(maddrs) == 1 {
		return maddrs[0].String()
	}
	maddrStrings := make([]string, len(maddrs))
	for i, maddr := range maddrs {
		maddrStrings[i] = maddr.String()
	}
	return strings.Join(maddrStrings, ", ")
}

func newClient(conn io.ReadWriteCloser, options ...p9.ClientOpt) (*p9.Client, error) {
	client, err := p9.NewClient(conn, options...)
	if err != nil {
		return nil, fmt.Errorf(
			"could not create client: %w",
			err,
		)
	}
	return client, nil
}

func launchAndConnect(exitInterval time.Duration, options ...p9.ClientOpt) (*p9.Client, error) {
	server, ipc, stderr, err := spawnDaemonProc(exitInterval)
	if err != nil {
		return nil, err
	}
	killProc := func() error {
		return errors.Join(
			maybeKill(server),
			server.Process.Release(),
		)
	}
	maddrs, err := getListenersFromProc(ipc, stderr, options...)
	if err != nil {
		errs := []error{err}
		if err := killProc(); err != nil {
			errs = append(errs, err)
		}
		return nil, errors.Join(errs...)
	}
	client, err := connect(maddrs)
	if err != nil {
		return nil, errors.Join(err, killProc())
	}
	if err := server.Process.Release(); err != nil {
		// If this branch is hit, something bad happened
		// at the system level. We can no longer assume
		// `Kill` is valid for this process, nor can we assume
		// stdio is still open and functional.
		// We can only defer this problem to the system's operator.
		return nil, errors.Join(err, client.Close())
	}
	return client, nil
}

func AutoStartDaemon(autoLaunch bool) Option {
	const name = "AutoStartDaemon"
	return func(settings *settings) error {
		if settings.autoLaunch {
			return generic.OptionAlreadySet(name)
		}
		settings.autoLaunch = autoLaunch
		return nil
	}
}

func WithAddress(maddr multiaddr.Multiaddr) Option {
	return func(settings *settings) error {
		settings.maddr = maddr
		return nil
	}
}

func WithExitInterval(interval time.Duration) Option {
	return func(settings *settings) error {
		settings.exitInterval = interval
		return nil
	}
}

func WithVerbosity(verbose bool) Option {
	const name = "WithVerbosity"
	return func(settings *settings) error {
		if settings.verbose {
			return generic.OptionAlreadySet(name)
		}
		settings.verbose = verbose
		return nil
	}
}

func (settings *settings) nineOpts() []p9.ClientOpt {
	if !settings.verbose {
		return nil
	}
	const (
		prefix = "9P - "
		flag   = 0
	)
	log := golog.New(os.Stdout, prefix, flag)
	return []p9.ClientOpt{p9.WithClientLogger(log)}
}
