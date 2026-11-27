//go:build unix

package dockerrt

import (
	"errors"
	"io/fs"
	"path/filepath"
	"syscall"
)

var errForeign = errors.New("чужой владелец")

// foreignOwned — в дереве dir есть файл или каталог не владельца uid.
func foreignOwned(dir string, uid int) bool {
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return errForeign // не прочитать — значит не наш
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != uid {
			return errForeign
		}
		return nil
	})
	return errors.Is(err, errForeign)
}
