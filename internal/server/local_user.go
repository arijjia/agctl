package server

// local_user.go — под каким пользователем работает контейнер рабочего места
// локального режима. На Linux bind-маунты не переводят владельца: процесс
// root в контейнере оставлял в папке проектов и dsh-home файлы root — agctl
// на хосте их не читал (permission denied), удалять приходилось через sudo.
// Поэтому на Linux контейнер работает под uid:gid хостового пользователя.
// Docker Desktop (macOS, Windows) владельцев переводит сам — там образ как
// есть (root). AGCTL_CONTAINER_USER: root — всегда пользователь образа, host
// — всегда хостовый (проверка на macOS), uid:gid — явно.

import (
	"fmt"
	"regexp"
	"strings"
)

var uidGidRe = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

// localContainerUser — "uid:gid" для контейнера или "" (пользователь образа).
func localContainerUser(goos, override string, uid, gid int) string {
	host := fmt.Sprintf("%d:%d", uid, gid)
	switch o := strings.TrimSpace(override); {
	case o == "root":
		return ""
	case o == "host":
		if uid < 0 || gid < 0 {
			return ""
		}
		return host
	case uidGidRe.MatchString(o):
		return o
	}
	if goos != "linux" || uid <= 0 || gid < 0 {
		return "" // Docker Desktop переводит владельцев сам; root на хосте — как есть
	}
	return host
}
