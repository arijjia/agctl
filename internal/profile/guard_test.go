package profile

// guard_test.go — машинная гарантия publishable-ядра: в коде agctl нет
// литералов конкретной организации. Кастомизация живёт только в данных
// профиля (profiles/) и в доках/бэклоге. Тест падает, если брендовые строки
// протекли в код — новые организации должны подключаться профилем, а не
// правками ядра.

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// orgMarkers — литералы, которых не должно быть в коде ядра. Имя профиля
// «utmn» как идентификатор разрешено (это не бренд, а ключ данных), брендовые
// тексты и корпоративный хост — нет.
var orgMarkers = []string{"ИИ-мастерская", "ТюмГУ", "Тюмень", "litellm-dev"}

// coreScopes — поверхности ядра, проверяемые guard'ом (относительно корня репо).
var coreScopes = []string{
	"cmd",
	"internal",
	"dsh-plugins",
	"web/src",
	"scripts",
	"bin",
	"system",
	"Makefile",
	"go.mod",
}

func TestCoreIsOrganizationFree(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	for _, scope := range coreScopes {
		abs := filepath.Join(root, scope)
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatalf("scope %s: %v", scope, err)
		}
		if !info.IsDir() {
			if hit := scanFile(abs, root); hit != "" {
				offenders = append(offenders, hit)
			}
			continue
		}
		err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if hit := scanFile(path, root); hit != "" {
				offenders = append(offenders, hit)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", scope, err)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("литералы организации в коде ядра (%d файлов) — кастомизация живёт в internal/profile/profiles или доках:\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}

// scanFile возвращает описание нарушения или "" (файл чистый/пропущен).
func scanFile(path, root string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)
	if !scanAllowed(rel) {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Бинарные файлы (логотипы и пр.) не сканируем.
	if bytes.IndexByte(raw[:min(len(raw), 8000)], 0) >= 0 {
		return ""
	}
	lower := strings.ToLower(string(raw))
	for _, m := range orgMarkers {
		if strings.Contains(lower, strings.ToLower(m)) {
			return rel + ": содержит " + m
		}
	}
	return ""
}

// scanAllowed — правила пропуска: данные профиля, контрактные тесты этого
// пакета (они специфицируют содержимое profiles/ и потому говорят языком
// данных), сторонние пакеты (кроме наших @arijjia-плагинов) и сгенерированные
// артефакты сборки.
func scanAllowed(rel string) bool {
	if strings.HasPrefix(rel, "internal/profile/profiles/") {
		return false // данные профиля — единственное место кастомизации
	}
	if strings.HasPrefix(rel, "internal/profile/") && strings.HasSuffix(rel, "_test.go") {
		return false // контрактные тесты данных профиля
	}
	if strings.Contains(rel, "node_modules") && !strings.Contains(rel, "@arijjia") {
		return false
	}
	switch {
	case strings.HasSuffix(rel, ".png"), strings.HasSuffix(rel, ".jpg"),
		strings.HasSuffix(rel, ".ico"), strings.HasSuffix(rel, ".woff2"),
		strings.HasSuffix(rel, ".pdf"):
		return false
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
