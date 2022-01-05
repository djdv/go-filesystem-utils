package environment

import (
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

type (
	Index interface {
		// TODO: channel outputs
		List(request *cmds.Request) ([]multiaddr.Multiaddr, error)
	}
	index struct {
		hostInstances instanceMap
	}
)

func (env *index) List(request *cmds.Request) ([]multiaddr.Multiaddr, error) {
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
