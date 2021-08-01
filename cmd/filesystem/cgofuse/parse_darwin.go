package cgofuse

import (
	"fmt"
	"os/user"
	"strings"

	"github.com/ipfs/go-ipfs/filesystem"
)

func ParseRequest(sysID filesystem.ID, target string) (request Request, err error) {
	// TODO reconsider if we should leave this in
	// macfuse accepts `~` literally
	// meaning input `~/target` creates a mountpoint of
	// `./~/target` not `/home/user/target`
	// we expand that here
	if strings.HasPrefix(target, "~") {
		usr, err := user.Current()
		if err != nil {
			panic(err)
		}
		target = usr.HomeDir + (target)[1:]
	}

	// basic Info
	request.HostPath = target
	opts := fmt.Sprintf("fsname=%s,volname=%s",
		sysID.String(),
		sysID.String(),
	)

	request.FuseArgs = append(request.FuseArgs, "-o", opts)
	return
}
