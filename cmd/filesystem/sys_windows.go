package fscmds

import (
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/sys/windows"
)

// NOTE: [MSDN]
// If an application requires normal Users to have write access to an application
// specific subdirectory of CSIDL_COMMON_APPDATA,
// then the application must explicitly modify the security
// on that sub-directory during application setup.
func makeServiceDir(serviceMaddr multiaddr.Multiaddr) error {
	if service.Interactive() {
		// directory already exists for user instances
		// TODO: we should check anyway
		return nil
	}
	// the system wide service directory is going to have more lax permissions
	// than our defaults would otherwise provide
	// TODO: these are TOO lax though, clamp down - for testing this is fine
	socketTarget, err := serviceMaddr.ValueForProtocol(multiaddr.P_UNIX)
	switch err {
	case multiaddr.ErrProtocolNotFound:
		return nil
	default:
		return err
	case nil:
		socketTarget = strings.TrimPrefix(socketTarget, `/`)
		socketDir := filepath.Dir(socketTarget)

		ownerSid, err := windows.CreateWellKnownSid(windows.WinCreatorOwnerRightsSid)
		if err != nil {
			return err
		}
		systemSid, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
		if err != nil {
			return err
		}
		adminsSid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
		if err != nil {
			return err
		}
		usersSid, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
		if err != nil {
			return err
		}

		group := windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(adminsSid),
		}

		aces := []windows.EXPLICIT_ACCESS{
			{
				AccessPermissions: windows.GENERIC_ALL,
				AccessMode:        windows.GRANT_ACCESS,
				Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
				Trustee: windows.TRUSTEE{
					TrusteeForm:  windows.TRUSTEE_IS_SID,
					TrusteeType:  windows.TRUSTEE_IS_USER,
					TrusteeValue: windows.TrusteeValueFromSID(ownerSid),
				},
			},
			{ // TODO: use FILE_ALL_ACCESS or less
				AccessPermissions: windows.GENERIC_ALL,
				AccessMode:        windows.GRANT_ACCESS,
				Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
				Trustee:           group,
			},
			{
				AccessPermissions: windows.GENERIC_ALL,
				AccessMode:        windows.GRANT_ACCESS,
				Inheritance:       windows.OBJECT_INHERIT_ACE,
				Trustee: windows.TRUSTEE{
					TrusteeForm:  windows.TRUSTEE_IS_SID,
					TrusteeType:  windows.TRUSTEE_IS_GROUP,
					TrusteeValue: windows.TrusteeValueFromSID(usersSid),
				},
			},
		}

		/* TODO: find out why this is returning an sd which forbids the creatdir call
		is it because one is self-relative and the other is absolute?
		sd, err := windows.BuildSecurityDescriptor(owner, group, aces, nil, nil)
		if err != nil {
			return err
		}
		for now, construct it manually
		*/

		sd, err := windows.NewSecurityDescriptor()
		if err != nil {
			return err
		}

		acl, err := windows.ACLFromEntries(aces, nil)
		if err != nil {
			return err
		}
		if err = sd.SetDACL(acl, true, false); err != nil {
			return err
		}
		if err = sd.SetOwner(systemSid, false); err != nil {
			return err
		}
		if err = sd.SetGroup(systemSid, false); err != nil {
			return err
		}
		if err = sd.SetSACL(nil, false, false); err != nil {
			return err
		}

		var sa windows.SecurityAttributes
		sa.Length = uint32(unsafe.Sizeof(sa))
		sa.SecurityDescriptor = sd

		socketDirPtr, err := windows.UTF16PtrFromString(socketDir)
		if err != nil {
			return err
		}
		return windows.CreateDirectory(socketDirPtr, &sa)
	}
}
