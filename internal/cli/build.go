package cli

import (
	"fmt"
	"runtime"
)

// Сборка подставляется точкой входа (main.version/commit/buildDate из
// ldflags) через SetBuild до разбора команд.
var (
	binName   = "agctl"
	version   = "dev"
	commit    = ""
	buildDate = ""
)

// SetBuild запоминает имя бинаря и сведения о сборке.
func SetBuild(name, ver, com, date string) {
	binName, version, commit, buildDate = name, ver, com, date
}

// Version — версия сборки (метка контейнеров, диагностика дрейфа).
func Version() string { return version }

// VersionLine — строка `<бинарь> version`.
func VersionLine() string {
	v := binName + " " + version
	if commit != "" {
		v += " " + commit
	}
	if buildDate != "" {
		v += " " + buildDate
	}
	return fmt.Sprintf("%s (%s/%s %s)", v, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
