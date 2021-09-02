package ipc

import (
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

// TODO: channel outputs
func (env *daemonEnvironment) List(request *cmds.Request) ([]multiaddr.Multiaddr, error) {
	var (
		mIndex    int
		instances = env.instances
		list      = make([]multiaddr.Multiaddr, len(instances))
	)
	for _, instance := range instances {
		list[mIndex] = instance.Target()
		mIndex++
	}

	return list, request.Context.Err()
}
