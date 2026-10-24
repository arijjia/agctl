// Package assets — встроенные в бинарь артефакты: шаблон создаваемой репы,
// скелет DSH_HOME и контекст docker-образа agctl-dsh.
package assets

import "embed"

// RepoTemplate — шаблон репки воркспейса (префикс "templates/repo").
//
//go:embed all:templates
var RepoTemplate embed.FS

// DshSkel — скелет DSH_HOME (префикс "dsh-skel").
//
//go:embed all:dsh-skel
var DshSkel embed.FS

// Image — контекст сборки образа agctl-dsh (префикс "image").
// Linux-бинарь agctl добавляется при сборке (make image / EnsureImage).
//
//go:embed all:image
var Image embed.FS

// BrandLogo — логотип «ИИ-мастерская ТюмГУ» для шлюзовых инъекций брендинга
// (тот же PNG, что зашит в плагин chat-brand; источник — images/image.png).
//
//go:embed brand/logo.png
var BrandLogo []byte
