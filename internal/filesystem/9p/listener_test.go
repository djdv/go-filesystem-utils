package p9_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
)

func TestListener(t *testing.T) {
	// TODO: proper tests+heir
	const listenerName = "testListener"
	var (
		ctx     = context.TODO()
		options = []p9fs.ListenerOption{
			p9fs.WithPath[p9fs.ListenerOption](new(atomic.Uint64)),
			p9fs.UnlinkEmptyChildren[p9fs.ListenerOption](true),
		}
		_, listener, listeners = p9fs.NewListener(ctx, options...)
	)
	kick := make(chan struct{})
	go func() {
		for l := range listeners {
			t.Log("got listener:", l.Multiaddr())
			<-kick
			if err := l.Close(); err != nil {
				t.Log(err)
			}
		}
	}()

	// TODO: test ports first,
	// and increment from some base if in-use
	// no wildcards.
	for _, maddr := range []multiaddr.Multiaddr{
		multiaddr.StringCast("/ip4/127.0.0.1/tcp/5523"),
	} {
		if err := p9fs.Listen(listener, maddr, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := tree(listener, 0); err != nil {
			t.Fatal(err)
		}
		kick <- struct{}{}
	}

	// TODO: properly sync tests; needs close to work properly first
	// we want to make sure listener.Close happens
	// and the error logs
	time.Sleep(1 * time.Second)
}

func tree(root p9.File, indent int) error {
	var (
		closers  = make([]io.Closer, 0)
		closeAll = func() error {
			for _, c := range closers {
				if err := c.Close(); err != nil {
					return err
				}
			}
			return nil
		}
	)
	defer closeAll() // TODO: dropped err
	ents, err := p9fs.ReadDir(root)
	if err != nil {
		return err
	}
	for _, ent := range ents {
		padding := strings.Repeat("\t", indent)
		name := ent.Name
		fmt.Println(padding, name)
		_, f, err := root.Walk([]string{name})
		if err != nil {
			return err
		}
		closers = append(closers, f)
		tree(f, indent+1)
	}
	return closeAll()
}
