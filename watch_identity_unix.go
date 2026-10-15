//go:build !windows && !plan9

package mygo

import (
	"os"
	"reflect"
	"syscall"
)

// fileWatchIdentity returns the identity os.SameFile compares, the device
// and inode of the os package's fileStat (os/types_unix.go). Another
// FileInfo, which os.SameFile takes for no file, has none.
func fileWatchIdentity(info os.FileInfo) (fileWatchID, bool) {
	t := reflect.TypeOf(info)
	if t == nil || t.Kind() != reflect.Pointer || t.Elem().Name() != "fileStat" || t.Elem().PkgPath() != "os" {
		return fileWatchID{}, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileWatchID{}, false
	}
	return fileWatchID{uint64(st.Dev), uint64(st.Ino)}, true
}
