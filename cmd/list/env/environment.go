package list

import (
	"github.com/djdv/go-filesystem-utils/filesystem"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

type (
	Environment interface {
		// TODO: channel outputs
		List(request *cmds.Request) ([]multiaddr.Multiaddr, error)
	}
	environment struct {
		hostInstances instanceMap
	}
	instanceMap map[string]filesystem.MountPoint
)

func MakeEnvironment() Environment { return &environment{} }

func (env *environment) List(request *cmds.Request) ([]multiaddr.Multiaddr, error) {
	var (
		mIndex    int
		instances = env.hostInstances
		list      = make([]multiaddr.Multiaddr, len(instances))
	)
	for _, instance := range instances {
		list[mIndex] = instance.Target()
		mIndex++
	}

	return list, request.Context.Err()
}
