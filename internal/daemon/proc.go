package daemon

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/files"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
)

func selfCommand(args []string, exitInterval time.Duration) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, args...)
	if exitInterval != 0 {
		cmd.Args = append(cmd.Args,
			fmt.Sprintf("-exit-after=%s", exitInterval),
		)
	}
	return cmd, nil
}

func startAndCommunicateWith(cmd *exec.Cmd, sio stdio) (multiaddr.Multiaddr, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stdClient, err := p9.NewClient(sio)
	if err != nil {
		// TODO: make sure cmd is stopped / stderr is closed for sure first.
		// ^This is typical, not guaranteed (proc is okay but server is not).
		stderr, sioErr := io.ReadAll(sio.err)
		if sioErr != nil {
			return nil, sioErr
		}
		if len(stderr) != 0 {
			err = fmt.Errorf("%w\nstderr:%s", err, stderr)
		}
		return nil, err
	}
	listenersDir, err := stdClient.Attach("listeners") // TODO: magic string -> const
	if err != nil {
		return nil, err
	}

	maddrs, err := flattenListeners(listenersDir)
	if err != nil {
		return nil, err
	}

	if err := stdClient.Close(); err != nil {
		return nil, err
	}
	if err := cmd.Process.Release(); err != nil {
		return nil, err
	}

	return maddrs[0], nil
}

func flattenListeners(dir p9.File) ([]multiaddr.Multiaddr, error) {
	ents, err := files.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var maddrs []multiaddr.Multiaddr
	for _, ent := range ents {
		wnames := []string{ent.Name}
		_, entFile, err := dir.Walk(wnames) // TODO: close?
		if err != nil {
			return nil, err
		}
		if ent.Type == p9.TypeDir {
			submaddrs, err := flattenListeners(entFile)
			if err != nil {
				return nil, err
			}
			maddrs = append(maddrs, submaddrs...)
			continue
		}

		maddrBytes, err := files.ReadAll(entFile)
		if err != nil {
			return nil, err
		}
		maddr, err := multiaddr.NewMultiaddrBytes(maddrBytes)
		if err != nil {
			return nil, err
		}
		maddrs = append(maddrs, maddr)
	}
	return maddrs, nil
}
