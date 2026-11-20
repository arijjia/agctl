// Package assets — встроенные в бинарь артефакты: скелет DSH_HOME
// и контекст docker-образа agctl-dsh. Каталог пакетов возможностей —
// содержимое организации и живёт в её профиле (catalog/).
package assets

import "embed"

// DshSkel — скелет DSH_HOME (префикс "dsh-skel").
//
//go:embed all:dsh-skel
var DshSkel embed.FS

// Image — контекст сборки образа agctl-dsh (префикс "image").
// Linux-бинарь agctl добавляется при сборке (make image / EnsureImage).
//
//go:embed all:image
var Image embed.FS
