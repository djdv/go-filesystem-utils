package fscmds

import (
	"context"
	"io"
	"sync"

	"github.com/ipfs/go-ipfs/filesystem/manager"
)

type (
	dispatchMap map[requestHeader]manager.Binder

	// commandDispatcher manages requests for/from `go-ipfs-cmds`.
	// Dispatching requests to one of several multiplexed binders.
	commandDispatcher struct {
		dispatchers dispatchMap
		instanceIndex
	}
)

type (
	indexKey = string
	indices  map[indexKey]*manager.Response

	instanceIndex interface {
		fetch(key indexKey) *manager.Response
		store(key indexKey, value *manager.Response)
		List(ctx context.Context) <-chan manager.Response
	}
	muIndex struct {
		sync.RWMutex
		indices
	}
)

type closer func() error      // io.Closer closure wrapper
func (f closer) Close() error { return f() }

func newIndex() instanceIndex { return &muIndex{indices: make(indices)} }

func (mi *muIndex) fetch(key indexKey) *manager.Response {
	mi.RLock()
	defer mi.RUnlock()
	return mi.indices[key]
}

func (mi *muIndex) store(key indexKey, value *manager.Response) {
	mi.Lock()
	defer mi.Unlock()
	mi.indices[key] = value

	maybeWrapCloser := func(original io.Closer) closer {
		if original == nil {
			return func() error { delete(mi.indices, key); return nil }
		}
		return func() error {
			delete(mi.indices, key)
			return original.Close()
		}
	}
	value.Closer = maybeWrapCloser(value.Closer)
}

func (mi *muIndex) List(ctx context.Context) <-chan manager.Response {
	mi.RLock()
	defer mi.RUnlock()
	respChan := make(chan manager.Response)
	go func() {
		defer close(respChan)
		for _, resp := range mi.indices {
			select {
			case respChan <- *resp:
			case <-ctx.Done():
				return
			}
		}
	}()
	return respChan
}
