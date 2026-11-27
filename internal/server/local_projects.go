package server

// local_projects.go — воркспейсы в локальном режиме. У каждого рабочего места
// своя папка на хосте, она же /workspace его контейнера: «Новый воркспейс» и
// «Возможности» работают в папке того места, из которого пришёл запрос, а не
// в папке основного. Место запроса — заголовок X-Agctl-Place (плагины dsh
// берут его из адреса /p/<id>/), запасной путь — Referer; без них —
// основное место.

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// placeHeader — id рабочего места запроса (плагины dsh локального режима).
const placeHeader = "X-Agctl-Place"

var placePathRe = regexp.MustCompile(`^/p/([a-z0-9][a-z0-9-]{0,31})/`)

// requestPlaceSlot — слот рабочего места, из которого пришёл запрос (0 —
// основное).
func (s *Server) requestPlaceSlot(r *http.Request, owner int64) (int, error) {
	id := strings.TrimSpace(r.Header.Get(placeHeader))
	if id == "" {
		if ref, err := url.Parse(r.Referer()); err == nil {
			if m := placePathRe.FindStringSubmatch(ref.Path); m != nil {
				id = m[1]
			}
		}
	}
	if id == "" || id == mainPlaceID {
		return 0, nil
	}
	slot, ok := s.placeSlot(owner, id)
	if !ok {
		return 0, fmt.Errorf("рабочее место %q не найдено", id)
	}
	return slot, nil
}

// localProjectRoot — где лежат воркспейсы места запроса: папка на хосте и
// путь к ней в контейнере dsh (/workspace, а с «путями как на хосте» —
// хостовый). ok=false — не локальный режим или не владелец: тогда проекты
// живут в томе пользователя, как на сервере.
func (s *Server) localProjectRoot(r *http.Request, uid int64) (host, inContainer string, ok bool, err error) {
	if !s.isLocal() || s.local == nil || uid != s.local.ownerID {
		return "", "", false, nil
	}
	slot, err := s.requestPlaceSlot(r, uid)
	if err != nil {
		return "", "", true, err
	}
	if slot == 0 {
		host = s.localWorkDir()
	} else {
		rec, err := s.localPlaceBySlot(chatKey{uid: uid, slot: slot})
		if err != nil {
			return "", "", true, err
		}
		if len(rec.Mounts) != 1 {
			return "", "", true, errors.New("у рабочего места несколько папок — новый воркспейс не создать в корне; выберите папку кнопкой «Существующая папка…»")
		}
		host = rec.Mounts[0].Host
	}
	if host == "" {
		return "", "", true, errors.New("у рабочего места нет папки проектов")
	}
	return host, s.hostPathsLayout(host, "/workspace"), true, nil
}

// localProjectPath — путь проекта name в контейнере места запроса.
func localProjectPath(inContainer, name string) string {
	return path.Join(inContainer, name)
}
