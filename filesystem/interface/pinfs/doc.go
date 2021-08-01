// Package pinfs provides a constructor to a `filesystem.Interface`,
// which itself provides access to a node's pins.
// Presenting them in the root directory.
// In addition, it acts as an IPFS proxy. Paths will be directed to an IPFS `filesystem.Interface`
// transparently.
//
// System paths are expected to follow the same convention as the ipfscore package's `filesystem.Interface`.
// e.g. `pinfs.Info("/Qm...")`
package pinfs
