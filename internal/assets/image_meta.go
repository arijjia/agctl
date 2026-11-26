package assets

// image_meta.go — что о встроенном образе знает сам бинарь: пин dsh и
// уровень возможностей guest. Лаунчер сравнивает с ними метки образа
// (io.agctl.*), чтобы показать, что внутри и не устарел ли образ.

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

// GuestAPI — уровень возможностей guest agctl в образе (метка
// io.agctl.guest.api, ARG AGCTL_GUEST_API в Dockerfile):
//
//	1 — мост к хосту (bridge);
//	2 — presets-sync в entrypoint: пресеты лаунчера видны в dsh.
//
// Повышается, когда платформе нужно новое от образа; образ ниже этого
// уровня лаунчер помечает «устарел — пересоберите».
const GuestAPI = 2

// Метки образа рабочего места (Dockerfile, последний слой).
const (
	LabelDshVersion    = "io.agctl.dsh.version"
	LabelGuestVersion  = "io.agctl.guest.version"
	LabelGuestRevision = "io.agctl.guest.revision"
	LabelGuestAPI      = "io.agctl.guest.api"
	LabelVariant       = "io.agctl.image.variant"
	LabelCreated       = "org.opencontainers.image.created"
)

var dshPinRe = regexp.MustCompile(`^ARG DSH_VERSION=([0-9][^"\s]*)`)

// PinnedDshVersion — пин dsh из встроенного Dockerfile полного образа
// (ARG DSH_VERSION=…; его двигает bin/dsh-bump.sh). Минимальный образ и
// образы по тегу-версии получают версию build-arg'ом.
func PinnedDshVersion() string {
	raw, err := Image.ReadFile("image/Dockerfile")
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		if m := dshPinRe.FindStringSubmatch(strings.TrimSpace(sc.Text())); m != nil {
			return m[1]
		}
	}
	return ""
}

var dshTagRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+-(alpha|beta|rc|next)\.[0-9]+$`)

// DshVersionFromTag — тег образа как версия dsh: agctl-dsh:0.2.3-alpha.2
// собирается на dsh 0.2.3-alpha.2. Только пререлизы: тег вида 0.2.2 —
// прежняя схема «версия линии образов» и пин из Dockerfile.
func DshVersionFromTag(tag string) string {
	if dshTagRe.MatchString(tag) {
		return tag
	}
	return ""
}
