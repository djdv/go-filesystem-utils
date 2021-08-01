// Package ipfscore provides a constructor to a `filesystem.Interface`
// which wraps (some of) the CoreAPI namespaces.
// Namely, IPFS and IPNS.
//
// System paths are expected to be provided sans namespace.
// e.g. `ipfs.Info("/Qm...")`, not `ipfs.Info("/ipfs/Qm...")`
package ipfscore
