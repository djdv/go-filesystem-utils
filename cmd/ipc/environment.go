package ipc

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	cmds "github.com/ipfs/go-ipfs-cmds"
	ipfsconfig "github.com/ipfs/go-ipfs-config"
	ipfsconfigfile "github.com/ipfs/go-ipfs-config/serialize"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
)

type (
	Environment interface {
		ServiceConfig(*cmds.Request) (*service.Config, error)
		Mount(*cmds.Request) ([]filesystem.MountPoint, error)
		List(*cmds.Request) ([]multiaddr.Multiaddr, error)
		Unmount(*cmds.Request) ([]multiaddr.Multiaddr, error)
		//Manager()
		//Mounter(*cmds.Request) (fscmds.Mounter, error)
	}

	binderPair struct {
		filesystem.API
		filesystem.ID
	}
	ipfsClientMap map[string]coreiface.CoreAPI
	binderMap     map[binderPair]filesystem.Mounter
	instanceMap   map[string]filesystem.MountPoint

	daemonEnvironment struct {
		context.Context
		ipfsClients ipfsClientMap
		mounters    binderMap
		instances   instanceMap
	}
)

func MakeEnvironment(ctx context.Context, request *cmds.Request) (cmds.Environment, error) {
	env := &daemonEnvironment{
		Context: ctx,
	}
	return env, nil
}

func CastEnvironment(environment cmds.Environment) (Environment, error) {
	typedEnv, isUsable := environment.(Environment)
	if !isUsable {
		interfaceName := reflect.TypeOf((*Environment)(nil)).Elem().Name()
		return nil, cmds.Errorf(cmds.ErrImplementation,
			"%T does not implement the %s interface",
			environment, interfaceName,
		)
	}
	return typedEnv, nil
}

func ipfsMaddrFromConfig() (multiaddr.Multiaddr, error) {
	confFile, err := ipfsconfig.Filename("") // TODO: argument from CLI?
	if err != nil {
		return nil, err
	}
	nodeConf, err := ipfsconfigfile.Load(confFile)
	if err != nil {
		return nil, err
	}

	// TODO: we should probably try dialing each of these and using the first to respond
	apiMaddrs := nodeConf.Addresses.API
	if len(apiMaddrs) == 0 {
		// TODO: real message
		return nil, errors.New("config has no API addrs")
	}

	return multiaddr.NewMultiaddr(apiMaddrs[0])
}

func resolveMaddr(ctx context.Context, addr multiaddr.Multiaddr) (multiaddr.Multiaddr, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, 10*time.Second)
	defer cancelFunc()

	addrs, err := madns.DefaultResolver.Resolve(ctx, addr)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		return nil, errors.New("non-resolvable API endpoint")
	}

	return addrs[0], nil
}

func ipfsClient(apiMaddr multiaddr.Multiaddr) (coreiface.CoreAPI, error) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFunc()
	resolvedMaddr, err := resolveMaddr(ctx, apiMaddr)
	if err != nil {
		return nil, err
	}
	return httpapi.NewApi(resolvedMaddr)
}

// lazy-alloc boilerplate
// TODO: mutexes; we may be called from multiple processes

func (m *ipfsClientMap) Add(maddr multiaddr.Multiaddr, api coreiface.CoreAPI) {
	clients := *m
	if clients == nil {
		clients = make(ipfsClientMap)
		*m = clients
	}
	clients[maddr.String()] = api
}

func (m ipfsClientMap) Get(maddr multiaddr.Multiaddr) coreiface.CoreAPI { return m[maddr.String()] }

func (m *binderMap) Add(pair binderPair, mounter filesystem.Mounter) {
	binder := *m
	if binder == nil {
		binder = make(binderMap)
		*m = binder
	}
	binder[pair] = mounter
}

func (m binderMap) Get(pair binderPair) filesystem.Mounter { return m[pair] }

func (m *instanceMap) Add(maddr multiaddr.Multiaddr, mountPoint filesystem.MountPoint) {
	mountPoints := *m
	if mountPoints == nil {
		mountPoints = make(instanceMap)
		*m = mountPoints
	}
	mountPoints[maddr.String()] = mountPoint
}

// TODO: lint - this needs to be List() []... - mutex guarded
//func (m instanceMap) Get(maddr multiaddr.Multiaddr) filesystem.MountPoint { return m[maddr.String()] }

func (m *instanceMap) Close(maddr multiaddr.Multiaddr) error {
	mounts := *m
	mountPoint := mounts[maddr.String()]
	if mountPoint == nil {
		return fmt.Errorf("TODO: real msg - can't close \"%s\", no instance found",
			maddr,
		)
	}
	err := mountPoint.Close()
	// NOTE: We stop tracking this mountPoint
	// whether Close succeeds or not.
	// If the instance did not detach from the host,
	// we can only defer to the system operator to do it themselves.
	// (`umount -f`, etc. depending on the platform)
	delete(mounts, maddr.String())
	return err
}
