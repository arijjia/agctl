// Package system содержит авторские артефакты карты системы: system/map/*.html —
// самодостаточные archify-диаграммы (источник — *.architecture.json рядом;
// пересборка — archify CLI: `finalize architecture agctl-processes.architecture.json
// agctl-processes.html --quality showcase`). Они отдаются agctl как страница /map —
// будущее админ-панели; сейчас это только карта.
package system

import (
	"embed"
	"io/fs"
)

//go:embed map/*.html
var mapFS embed.FS

// Map возвращает файловую систему собранных диаграмм (map/*.html).
func Map() fs.FS {
	sub, err := fs.Sub(mapFS, "map")
	if err != nil {
		panic(err) // статический embed: ошибка возможна только при сборке
	}
	return sub
}
