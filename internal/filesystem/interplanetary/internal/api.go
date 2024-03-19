package interplanetary

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	giconfig "github.com/ipfs/kubo/config" // Global IPFS config.
	"github.com/multiformats/go-multiaddr"
)

const (
	ipfsConfigAPIFileName = "api"
	ipfsConfigEnv         = giconfig.EnvDir
	ipfsConfigDir         = giconfig.DefaultPathRoot
)

func IPFSAPIs() ([]multiaddr.Multiaddr, error) {
	location, err := getIPFSAPIPath()
	if err != nil {
		return nil, err
	}
	apiFileExists := func() bool {
		_, err := os.Stat(location)
		return err == nil
	}()
	if !apiFileExists {
		return nil, generic.ConstError("IPFS API file not found (daemon not running?)")
	}
	return parseIPFSAPI(location)
}

func getIPFSAPIPath() (string, error) {
	var target string
	if ipfsPath, set := os.LookupEnv(ipfsConfigEnv); set {
		target = filepath.Join(ipfsPath, ipfsConfigAPIFileName)
	} else {
		target = filepath.Join(ipfsConfigDir, ipfsConfigAPIFileName)
	}
	return expandHomeShorthand(target)
}

func expandHomeShorthand(name string) (string, error) {
	if !strings.HasPrefix(name, "~") {
		return name, nil
	}
	homeName, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeName, name[1:]), nil
}

func parseIPFSAPI(name string) ([]multiaddr.Multiaddr, error) {
	// NOTE: [upstream problem]
	// If the node's config file has multiple API maddrs defined,
	// only the first one will be contained in the API file.
	// If this gets fixed upstream, we need to use a scanner
	// for whatever format they decide to use
	// (line delimited, csv, etc.).
	maddrString, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	maddr, err := multiaddr.NewMultiaddr(string(maddrString))
	if err != nil {
		return nil, err
	}
	return []multiaddr.Multiaddr{maddr}, nil
}
