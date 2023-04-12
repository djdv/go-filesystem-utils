package p9_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

func TestListener(t *testing.T) {
	t.Parallel()
	t.Run("default", listenerDefault)
	t.Run("options", listenerWithOptions)

}

// best effort, not guaranteed to actually
// be a free port on all systems.
func getTCPPort(t *testing.T, address string) int {
	const network = "tcp"
	stdListener, err := net.Listen(network, address+":0")
	if err != nil {
		t.Fatalf("could not listen via std: %v", err)
	}
	port := stdListener.Addr().(*net.TCPAddr).Port
	if err := stdListener.Close(); err != nil {
		t.Fatalf("could not close std listener: %v", err)
	}
	return port
}

func newTCPMaddr(t *testing.T, netIntf string) multiaddr.Multiaddr {
	port := getTCPPort(t, netIntf)
	return multiaddr.StringCast(fmt.Sprintf("/ip4/%s/tcp/%d", netIntf, port))
}

func listenerDefault(t *testing.T) {
	t.Parallel()
	const address = "127.0.0.1"
	var (
		maddr       = newTCPMaddr(t, address)
		ctx, cancel = context.WithCancel(context.Background())
	)
	defer cancel()
	_, listenerDir, listeners, lErr := p9fs.NewListener(ctx)
	if lErr != nil {
		t.Fatalf("could not create listener directory: %v", lErr)
	}
	listenerTCPServiceTest(t, listenerDir, listeners, maddr)
	// Directories should still exist after listener closes
	// since options were not specified.
	var (
		names   = maddrToNames(maddr)
		trimmed = names[:len(names)-1]
	)
	mustWalkTo(t, listenerDir, trimmed)
}

func listenerWithOptions(t *testing.T) {
	t.Parallel()
	const address = "127.0.0.1"
	var (
		maddr       = newTCPMaddr(t, address)
		ctx, cancel = context.WithCancel(context.Background())
	)
	defer cancel()
	_, listenerDir, listeners, lErr := p9fs.NewListener(ctx,
		p9fs.UnlinkEmptyChildren[p9fs.ListenerOption](true),
		p9fs.WithBuffer[p9fs.ListenerOption](1),
	)
	if lErr != nil {
		t.Fatalf("could not create listener directory: %v", lErr)
	}
	// This shouldn't hang because we requested a buffer.
	const permissions = 0o751
	maddr2 := newTCPMaddr(t, address)
	if err := p9fs.Listen(listenerDir, maddr2, permissions); err != nil {
		t.Fatalf("could not listen on %v: %v", maddr, err)
	}
	// We don't need to background this, again because of the buffer.
	listener := <-listeners // Hold on to this while we test with another listener.

	listenerTCPServiceTest(t, listenerDir, listeners, maddr)

	// Directories should still exist after listener closes
	// since `listener` is also still active.
	var (
		names   = maddrToNames(maddr)
		trimmed = names[:len(names)-1]
	)
	mustWalkTo(t, listenerDir, trimmed)

	// This should trigger a cleanup since no
	// other listeners are using this chain of protocols.
	if err := listener.Close(); err != nil {
		t.Fatalf("could not close listener: %v", err)
	}

	// Root should be empty after listener closes
	// since cleanup options were provided and no other
	// entry was added by this test.
	ents, err := p9fs.ReadDir(listenerDir)
	if err != nil {
		t.Fatalf("could not read directory: %v", err)
	}
	if entCount := len(ents); entCount != 0 {
		t.Fatalf("directory should be empty"+
			"\ngot: %v"+
			"\nwant: %v",
			ents, nil,
		)
	}
}

func listenerTCPServiceTest(t *testing.T, listenerDir p9.File, listeners <-chan manet.Listener, maddr multiaddr.Multiaddr) {
	var (
		errs    = make(chan error)
		payload = []byte("arbitrary data")
	)
	go func() {
		defer close(errs)
		listener := <-listeners
		if err := listenerMatches(listener, maddr); err != nil {
			errs <- err
		}
		/*
			if err := listenerExists(listenerDir, maddr); err != nil {
				errs <- err
			}
		*/
		if err := <-listenerHostEchoTCP(listener, payload); err != nil {
			errs <- err
		}
		if err := listener.Close(); err != nil {
			errs <- err
		}
		/*
			if err := listenerNotExist(listenerDir, maddr); err != nil {
				errs <- err
			}
		*/
	}()
	const permissions = 0o751
	if err := p9fs.Listen(listenerDir, maddr, permissions); err != nil {
		t.Fatalf("could not listen on %v: %v", maddr, err)
	}
	listenerClientEchoTCP(t, maddr, payload)
	var err error
	for e := range errs {
		err = errors.Join(e)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func listenerHostEchoTCP(listener manet.Listener, expected []byte) <-chan error {
	errs := make(chan error, 1)
	go func() {
		var rErr error
		defer func() {
			if rErr != nil {
				errs <- rErr
			}
			close(errs)
		}()
		conn, err := listener.Accept()
		if err != nil {
			rErr = fmt.Errorf("could not accept: %v", err)
			return
		}
		data := make([]byte, len(expected))
		read, err := conn.Read(data)
		if err != nil {
			rErr = fmt.Errorf("could not read from connection: %v", err)
			return
		}
		if want := len(expected); read != want {
			rErr = fmt.Errorf("mismatched number of bytes read"+
				"\ngot: %d"+
				"\nwant: %d",
				read, want,
			)
			return
		}
		if !bytes.Equal(data, expected) {
			rErr = fmt.Errorf("mismatched data read"+
				"\ngot: %v"+
				"\nwant: %v",
				data, expected,
			)
			return
		}
	}()
	return errs
}

func listenerClientEchoTCP(t *testing.T, maddr multiaddr.Multiaddr, payload []byte) {
	conn, err := manet.Dial(maddr)
	if err != nil {
		t.Fatalf("could not dial: %v", err)
	}
	wrote, err := conn.Write(payload)
	if err != nil {
		t.Fatalf("could not write to connection: %v", err)
	}
	if want := len(payload); wrote != want {
		t.Fatalf("mismatched number of bytes written"+
			"\ngot: %d"+
			"\nwant: %d",
			wrote, len(payload),
		)
	}
}

func maddrToNames(maddr multiaddr.Multiaddr) []string {
	return strings.Split(maddr.String(), "/")[1:]
}

func listenerMatches(listener manet.Listener, maddr multiaddr.Multiaddr) error {
	if lMaddr := listener.Multiaddr(); !maddr.Equal(lMaddr) {
		return fmt.Errorf("mismatched listener address"+
			"\ngot: %v"+
			"\nwant: %v",
			lMaddr, maddr,
		)
	}
	return nil
}

func listenerExists(listenerDir p9.File, maddr multiaddr.Multiaddr) error {
	var (
		names             = maddrToNames(maddr)
		listenerFile, err = walkTo(listenerDir, names)
	)
	cErr := listenerDir.Close()
	if cErr != nil {
		cErr = fmt.Errorf("could not close listenerDir: %v", err)
	}
	if err == nil {
		if lErr := listenerFile.Close(); lErr != nil {
			err = fmt.Errorf("could not close listenerFile: %v", lErr)
		}
	}
	return errors.Join(cErr, err)
}

func mustWalkTo(t *testing.T, file p9.File, names []string) {
	file, err := walkTo(file, names)
	if err != nil {
		t.Fatalf("could not walk to directory: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("could not close directory: %v", err)
	}
}

func listenerNotExist(listenerDir p9.File, maddr multiaddr.Multiaddr) (err error) {
	var (
		names                = maddrToNames(maddr)
		trimmed              = names[:len(names)-1]
		listenerParent, wErr = walkTo(listenerDir, trimmed)
	)
	if wErr != nil {
		return fmt.Errorf("could not walk to parent directory: %v", wErr)
	}
	defer func() {
		if cErr := listenerParent.Close(); cErr != nil {
			cErr = fmt.Errorf("could not close file: %v", cErr)
			err = errors.Join(err, cErr)
		}
	}()
	ents, rErr := p9fs.ReadDir(listenerParent)
	if rErr != nil {
		return fmt.Errorf("could not read directory: %v", rErr)
	}
	fileName := names[len(names)-1]
	for _, ent := range ents {
		if ent.Name == fileName {
			return fmt.Errorf("file %s should not exist after listener is closed", fileName)
		}
	}
	return nil
}

func walkTo(root p9.File, names []string) (p9.File, error) {
	_, file, err := root.Walk(names)
	return file, err
}
