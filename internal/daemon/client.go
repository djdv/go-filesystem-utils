package daemon

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/u-root/uio/ulog"
)

type (
	nanoidGen = func() string
	Client    struct {
		p9Client *p9.Client
		log      ulog.Logger
		idGen    nanoidGen
	}
)

func Connect(serverMaddr multiaddr.Multiaddr, options ...ClientOption) (*Client, error) {
	client := new(Client)
	for _, setFunc := range options {
		if err := setFunc(client); err != nil {
			panic(err)
		}
	}

	conn, err := manet.Dial(serverMaddr)
	if err != nil {
		return nil, err
	}

	var clientOpts []p9.ClientOpt
	if clientLog := client.log; clientLog != nil {
		clientOpts = []p9.ClientOpt{
			p9.WithClientLogger(clientLog),
		}
	}

	p9Client, err := p9.NewClient(conn, clientOpts...)
	if err != nil {
		return nil, err
	}
	client.p9Client = p9Client
	return client, nil
}

func ConnectOrLaunchLocal(options ...ClientOption) (*Client, error) {
	serverMaddr, err := FindLocalServer()
	if err == nil {
		return Connect(serverMaddr, options...)
	}
	if !errors.Is(err, ErrServiceNotFound) {
		return nil, err
	}
	// TODO: const for daemon CLI cmd name
	return SelfConnect([]string{"daemon"}, options...)
}


func (c *Client) Shutdown(maddr multiaddr.Multiaddr) error {
	if c.p9Client == nil {
		// TODO: better message; maybe better logic?
		// Can we prevent this from being possible without unexporting [Client]?
		return fmt.Errorf("client is not connected")
	}

	// TODO: const name in files pkg?
	listenersDir, err := c.p9Client.Attach("listeners")
	if err != nil {
		return err
	}
	var names []string
	multiaddr.ForEach(maddr, func(c multiaddr.Component) bool {
		names = append(names, strings.Split(c.String(), "/")[1:]...)
		return true
	})
	tail := len(names) - 1
	_, dir, err := listenersDir.Walk(names[:tail])
	if err != nil {
		return err
	}
	if err := dir.UnlinkAt(names[tail], 0); err != nil {
		return err
	}
	if err := dir.Close(); err != nil {
		return err
	}
	if err := listenersDir.Close(); err != nil {
		return err
	}
	return nil
}

func (c *Client) Close() error {
	cl := c.p9Client
	if cl == nil {
		// TODO: better message; maybe better logic?
		// Can we prevent this from being possible without unexporting [Client]?
		return fmt.Errorf("client is not connected")
	}
	c.p9Client = nil
	return cl.Close()
}

// FindLocalServer searches a set of local addresses
// and returns the first dialable maddr it finds.
// Otherwise it returns [ErrServiceNotFound].
func FindLocalServer() (multiaddr.Multiaddr, error) {
	userMaddrs, err := UserServiceMaddrs()
	if err != nil {
		return nil, err
	}
	systemMaddrs, err := SystemServiceMaddrs()
	if err != nil {
		return nil, err
	}

	var (
		localDefaults = append(userMaddrs, systemMaddrs...)
		maddrStrings  = make([]string, len(localDefaults))
	)
	for i, serviceMaddr := range localDefaults {
		if Dialable(serviceMaddr) {
			return serviceMaddr, nil
		}
		maddrStrings[i] = serviceMaddr.String()
	}

	return nil, fmt.Errorf("%w: tried %s",
		ErrServiceNotFound, strings.Join(maddrStrings, ", "),
	)
}

// Dialable returns true if the multiaddr was dialed without error.
func Dialable(maddr multiaddr.Multiaddr) (connected bool) {
	conn, err := manet.Dial(maddr)
	if err == nil && conn != nil {
		if err := conn.Close(); err != nil {
			return // Socket is faulty, not accepting.
		}
		connected = true
	}
	return
}
