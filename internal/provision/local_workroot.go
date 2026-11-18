package provision

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// Локальный режим (agctl dev): work-корень владельца — родная папка проектов
// разработчика, а не chats/u<ID>/work в доме платформы. Подмена задаётся
// сервером при старте и действует только для пары (дом, пользователь).
var (
	localRootsMu sync.RWMutex
	localRoots   = map[string]string{}
)

func localRootKey(home string, userID int64) string {
	if abs, err := filepath.Abs(home); err == nil {
		home = abs
	}
	return home + "\x00" + strconv.FormatInt(userID, 10)
}

// SetLocalWorkRoot задаёт work-корень пользователя локального режима
// (абсолютный существующий каталог); пустой dir снимает подмену.
func SetLocalWorkRoot(home string, userID int64, dir string) {
	localRootsMu.Lock()
	defer localRootsMu.Unlock()
	if dir == "" {
		delete(localRoots, localRootKey(home, userID))
		return
	}
	localRoots[localRootKey(home, userID)] = dir
}

func localWorkRoot(home string, userID int64) (string, bool) {
	localRootsMu.RLock()
	dir, ok := localRoots[localRootKey(home, userID)]
	localRootsMu.RUnlock()
	if !ok {
		return "", false
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", false
	}
	return dir, true
}
