package mygo

import (
	"os"
	"reflect"
	"sync"
)

// fileStatFields are the indices of the fields of os.fileStat that
// os.SameFile compares (os/types_windows.go), found once; ok is false
// when they aren't there as expected.
var fileStatFields struct {
	once                    sync.Once
	ok                      bool
	path, vol, idxhi, idxlo int
}

// fileWatchIdentity returns the identity os.SameFile compares, the volume
// and file index of the os package's fileStat, once it holds them: its
// path is empty when they were read from a handle or by StatIdentity.
// Another FileInfo, or one whose ID is still to be read, has none.
func fileWatchIdentity(info os.FileInfo) (fileWatchID, bool) {
	v := reflect.ValueOf(info)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fileWatchID{}, false
	}
	v = v.Elem()
	t := v.Type()
	if t.Kind() != reflect.Struct || t.Name() != "fileStat" || t.PkgPath() != "os" {
		return fileWatchID{}, false
	}
	f := &fileStatFields
	f.once.Do(func() {
		index := func(name string, kind reflect.Kind) int {
			field, ok := t.FieldByName(name)
			if !ok || field.Type.Kind() != kind || len(field.Index) != 1 {
				return -1
			}
			return field.Index[0]
		}
		f.path, f.vol, f.idxhi, f.idxlo = index("path", reflect.String), index("vol", reflect.Uint32), index("idxhi", reflect.Uint32), index("idxlo", reflect.Uint32)
		f.ok = f.path >= 0 && f.vol >= 0 && f.idxhi >= 0 && f.idxlo >= 0
	})
	if !f.ok || v.Field(f.path).Len() != 0 {
		return fileWatchID{}, false
	}
	return fileWatchID{v.Field(f.vol).Uint(), v.Field(f.idxhi).Uint()<<32 | v.Field(f.idxlo).Uint()}, true
}
