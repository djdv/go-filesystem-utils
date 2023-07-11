package commands

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/adrg/xdg"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/sys/windows"
)

func bindServiceControlFlags(flagSet *flag.FlagSet, options *controlOptions) {
	// NOTE: Key+value types defined on
	// [service.KeyValue] documentation.
	const (
		passwordName  = serviceFlagPrefix + "password"
		passwordUsage = "password to use when interfacing with the service manager"
	)
	flagSetFunc(flagSet, passwordName, passwordUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["Password"] = value
			return nil
		})
	const (
		delayedName  = serviceFlagPrefix + "delayed-auto-start"
		delayedUsage = "delay the service from starting (immediately after boot)"
	)
	flagSetFunc(flagSet, delayedName, delayedUsage, options,
		func(value bool, settings *controlSettings) error {
			settings.Config.Option["DelayedAutoStart"] = value
			return nil
		})
}

func createServiceMaddrs() ([]multiaddr.Multiaddr, cleanupFunc, error) {
	serviceDir := filepath.Join(
		xdg.ConfigDirs[0],
		serverRootName,
	)
	cleanup, err := createSystemServiceDirectory(serviceDir)
	if err != nil {
		return nil, nil, err
	}
	serviceMaddr, err := multiaddr.NewMultiaddr(
		"/unix/" + filepath.Join(serviceDir, serverName),
	)
	if err == nil {
		return []multiaddr.Multiaddr{serviceMaddr}, cleanup, nil
	}
	if cleanup != nil {
		if cErr := cleanup(); cErr != nil {
			err = errors.Join(err, cErr)
		}
	}
	return nil, nil, err
}

// NOTE: On permissions.
// 1) [MSDN]
// "If an application requires normal Users to have write access to an application
// specific subdirectory of CSIDL_COMMON_APPDATA,
// then the application must explicitly modify the security
// on that sub-directory during application setup."
//
// 2)
// WSA currently (v19043.928) requires
// read, write, and delete for `connect` to succeed with unix domain sockets.
// We allow to be able to do that, so we allow that access
// on files underneath the service directory.

func createSystemServiceDirectory(serviceDir string) (cleanupFunc, error) {
	var systemSid, adminSid, usersSid *windows.SID
	{
		sids := []**windows.SID{&systemSid, &adminSid, &usersSid}
		for i, sid := range []windows.WELL_KNOWN_SID_TYPE{
			windows.WinLocalSystemSid,
			windows.WinBuiltinAdministratorsSid,
			windows.WinBuiltinUsersSid,
		} {
			var err error
			if *(sids[i]), err = windows.CreateWellKnownSid(sid); err != nil {
				return nil, err
			}
		}
	}
	dacl, err := makeServiceACL(systemSid, usersSid)
	if err != nil {
		return nil, err
	}
	securityAttributes, err := makeServiceSecurityAttributes(systemSid, adminSid, dacl)
	if err != nil {
		return nil, err
	}
	pszServiceDir, err := windows.UTF16PtrFromString(serviceDir)
	if err != nil {
		return nil, err
	}
	// NOTE: Regardless of the state of the file system;
	// we're about to own (and later destroy) this directory.
	// The directory shouldn't exist, but if the caller fails
	// to call cleanup, it could on a subsequent run.
	// Most users will not have delete access, so instead of failing
	// we make an exception here and just take ownership.
	// This allows the calling code to be patched. Afterwards
	// the service can be restarted, and the (erroneously) leftover
	// directory will be clobbered.
	// TODO: reconsider this, we've bulletproofed the daemon somewhat.
	// service should fail if this exists.
	if err = windows.CreateDirectory(pszServiceDir, securityAttributes); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		// Don't remake the service directory,
		// but set its security to the permissions we need.
		if err = windows.SetNamedSecurityInfo(serviceDir, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION,
			systemSid, systemSid,
			dacl, nil); err != nil {
			return nil, err
		}
	}
	// Allow the service directory to be deleted
	// when the caller is done with it.
	cleanup := func() error { return os.Remove(serviceDir) }
	return cleanup, nil
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
	dacl *windows.ACL,
) (*windows.SecurityAttributes, error) {
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
