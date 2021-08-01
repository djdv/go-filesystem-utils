// Package ufs provides a constructor to a `filesystem.Interface`,
// which itself provides a means to open and modify UFS files.
//
// TODO: explain modified func, either here or on its docstring.
//
// System paths are expected to reference a UFS file, and may be any canoncical IPFS path.
// e.g. `ufs.Open("/Qm...")` (IPFS namespace implied), `ufs.Open("/ipns/Qm...")`,
package ufs
