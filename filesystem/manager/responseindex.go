package manager

import (
	"context"
	"io"
	"sync"

	"github.com/multiformats/go-multiaddr"
)

type (
	indexKey = multiaddr.Multiaddr

	muIndices map[string]*Response
	muIndex   struct {
		sync.RWMutex
		indices muIndices
	}
)

func NewIndex() Index { return &muIndex{indices: make(muIndices)} }

func (mi *muIndex) fetch(key indexKey) *Response {
	mi.RLock()
	defer mi.RUnlock()

	return mi.indices[key.String()]
}

type closer func() error          // io.Closer wrapper, call self
func (close closer) Close() error { return close() }

func (mi *muIndex) store(key indexKey, value *Response) {
	mi.Lock()
	defer mi.Unlock()

	var (
		keyStr          = key.String()
		removeFromIndex = func() error {
			mi.Lock()
			defer mi.Unlock()
			delete(mi.indices, keyStr)
			return nil
		}
		maybeWrapCloser = func(originalCloser io.Closer) closer {
			if originalCloser == nil {
				return removeFromIndex
			}
			return func() error {
				removeFromIndex()
				return originalCloser.Close()
			}
		}
	)

	mi.indices[keyStr] = value
	// when this object is closed,
	// remove it from the index
	value.Closer = maybeWrapCloser(value.Closer)
}

func (mi *muIndex) List(ctx context.Context) <-chan Response {
	mi.RLock()
	defer mi.RUnlock()
	respChan := make(chan Response)
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
