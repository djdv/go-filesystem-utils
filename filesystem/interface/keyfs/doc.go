// Package keyfs provides a constructor to a `filesystem.Interface`,
// which itself provides mutable access to a node's keys.
// Presenting them in the root directory as their string names.
// In addition, it acts as an IPNS proxy. Paths who's first component does not match a key
// will be directed to an IPNS `filesystem.Interface` transparently.
//
// System paths are expected to start with either a key's name, or be an IPNS path, sans namespace.
// e.g. `keyfs.Info("/myKey")`, or `keyfs.Info("/Qm...")`
package keyfs
