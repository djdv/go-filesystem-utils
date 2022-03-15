package service

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/adrg/xdg"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"golang.org/x/sys/windows"
)

func systemListeners(maddrsProvided bool, sysLog service.Logger) (serviceListeners []manet.Listener,
	cleanup func() error, err error) {
	if maddrsProvided {
		return // Supply nothing; let the daemon instantiate from the arguments.
	}

	defer func() { // NOTE: Read+Writes to named return value.
		if err != nil {
			err = cleanupAndLog(sysLog, cleanup, err)
		}
	}()

	serviceDir := filepath.Join(
		xdg.ConfigDirs[len(xdg.ConfigDirs)-1],
		daemon.ServerRootName,
	)
	if cleanup, err = createSystemServiceDirectory(serviceDir); err != nil {
		return
	}

	serviceListeners, err = systemServiceListen(serviceDir)
	return
}

/* NOTE:
1) [MSDN]
"If an application requires normal Users to have write access to an application
specific subdirectory of CSIDL_COMMON_APPDATA,
then the application must explicitly modify the security
on that sub-directory during application setup."

2)
WSA currently (v19043.928) requires
read, write, and delete for `connect` to succeed with unix domain sockets.
We allow to be able to do that, so we allow that access
on files underneath the service directory.
*/
func createSystemServiceDirectory(serviceDir string) (cleanup func() error, err error) {
	var (
		systemSid, adminSid, usersSid *windows.SID
		dacl                          *windows.ACL
		securityAttributes            *windows.SecurityAttributes
		pszServiceDir                 *uint16
	)
	{
		sids := []**windows.SID{&systemSid, &adminSid, &usersSid}
		for i, sid := range []windows.WELL_KNOWN_SID_TYPE{
			windows.WinLocalSystemSid,
			windows.WinBuiltinAdministratorsSid,
			windows.WinBuiltinUsersSid,
		} {
			if *(sids[i]), err = windows.CreateWellKnownSid(sid); err != nil {
				return
			}
		}
	}

	if dacl, err = makeServiceACL(systemSid, usersSid); err != nil {
		return
	}
	if securityAttributes, err = makeServiceSecurityAttributes(systemSid, adminSid, dacl); err != nil {
		return
	}
	if pszServiceDir, err = windows.UTF16PtrFromString(serviceDir); err != nil {
		return
	}

	// NOTE: Regardless of the state of the file system;
	// we're about to own (and later destroy) this directory.
	if err = windows.CreateDirectory(pszServiceDir, securityAttributes); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return
		}
		// Don't remake the service directory,
		// but set its security to the permissions we need.
		if err = windows.SetNamedSecurityInfo(serviceDir, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION,
			systemSid, systemSid,
			dacl, nil); err != nil {
			return
		}
	}
	// Allow the service directory to be deleted
	// when the caller is done with it.
	cleanup = func() error { return os.Remove(serviceDir) }
	return
}

func makeServiceACL(ownerSid, clientSid *windows.SID) (*windows.ACL, error) {
	aces := []windows.EXPLICIT_ACCESS{
		{ // Service level 0+ (/**)
			// Grant ALL ...
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT, // recursively ...
			Trustee: windows.TRUSTEE{ // ... to the service owner.
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(ownerSid),
			},
		},
		{ // Level 1 - (/*)
			// Grant permissions required to operate Unix socket objects ...
			AccessPermissions: windows.GENERIC_READ |
				windows.GENERIC_WRITE |
				windows.DELETE,
			AccessMode: windows.GRANT_ACCESS,
			Inheritance: windows.INHERIT_ONLY_ACE | // but not in our container (Level 0) ...
				windows.OBJECT_INHERIT_ACE | // and only to objects (files in Level 1) ...
				windows.INHERIT_NO_PROPAGATE, // and not in levels 2+ ...
			Trustee: windows.TRUSTEE{ // ... to clients of this scope.
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(clientSid),
			},
		},
	}
	return windows.ACLFromEntries(aces, nil)
}

func makeServiceSecurityAttributes(ownerSid, groupSid *windows.SID,
	dacl *windows.ACL) (*windows.SecurityAttributes, error) {
	securityDesc, err := windows.NewSecurityDescriptor()
	if err != nil {
		return nil, err
	}

	if err := securityDesc.SetDACL(dacl, true, false); err != nil {
		return nil, err
	}
	if err := securityDesc.SetOwner(ownerSid, false); err != nil {
		return nil, err
	}
	if err := securityDesc.SetGroup(groupSid, false); err != nil {
		return nil, err
	}

	securityAttributes := new(windows.SecurityAttributes)
	securityAttributes.Length = uint32(unsafe.Sizeof(*securityAttributes))
	securityAttributes.SecurityDescriptor = securityDesc

	return securityAttributes, nil
}

func systemServiceListen(serviceDir string) ([]manet.Listener, error) {
	var (
		socketPath      = filepath.Join(serviceDir, daemon.ServerName)
		multiaddrString = "/unix/" + socketPath
		serviceMaddr    multiaddr.Multiaddr
		listener        manet.Listener
	)
	serviceMaddr, err := multiaddr.NewMultiaddr(multiaddrString)
	if err != nil {
		return nil, err
	}
	listener, err = manet.Listen(serviceMaddr)
	if err != nil {
		return nil, err
	}

	return []manet.Listener{listener}, nil
}
