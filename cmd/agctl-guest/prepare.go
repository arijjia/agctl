package main

// prepare.go — `agctl prepare`: перед стартом dsh (entrypoint образа)
// отмечает, какой версией dsh и guest писался dsh-home, и предупреждает о
// смене версии: апгрейд — строкой в логе, откат на более старый dsh — явным
// предупреждением (сессии, записанные новой версией, старая может не
// открыть; безопасный путь — пробовать версии на клоне места). Метка —
// $DSH_HOME/.agctl/state.json, последние смены версий — в её истории.
// Ничего не блокирует: dsh стартует в любом случае.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/cli"
)

// dshPackageJSON — package.json установленного в образе dsh.
const dshPackageJSON = "/usr/local/lib/node_modules/@deepseek-ai/dsh/package.json"

// homeState — метка версий dsh-home.
type homeState struct {
	Dsh     string         `json:"dsh"`
	Guest   string         `json:"guest"`
	Updated string         `json:"updated"`
	History []stateChange  `json:"history,omitempty"`
	Extra   map[string]any `json:"-"`
}

type stateChange struct {
	From string `json:"from"`
	To   string `json:"to"`
	At   string `json:"at"`
}

func cmdPrepare(args []string) {
	fs := flag.NewFlagSet("prepare", flag.ExitOnError)
	home := fs.String("dsh-home", os.Getenv("DSH_HOME"), "DSH_HOME рабочего места")
	pkg := fs.String("dsh-package", dshPackageJSON, "package.json установленного dsh")
	_ = fs.Parse(args)
	for _, line := range prepareHome(*home, *pkg, cli.Version()+" "+cli.Commit(), time.Now()) {
		fmt.Println(line)
	}
}

// prepareHome — проверка и метка версий; возвращает строки для лога.
func prepareHome(home, pkgPath, guest string, now time.Time) []string {
	if home == "" {
		return []string{"prepare: DSH_HOME не задан — пропускаю"}
	}
	var out []string
	cur := installedDshVersion(pkgPath)
	if cur == "" {
		return []string{"prepare: ⚠ версия dsh в образе не определена (" + pkgPath + ")"}
	}
	dir := filepath.Join(home, ".agctl")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return []string{"prepare: ⚠ dsh-home не пишется: " + err.Error()}
	}
	path := filepath.Join(dir, "state.json")
	var st homeState
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	stamp := now.UTC().Format(time.RFC3339)
	switch c := compareVersions(cur, st.Dsh); {
	case st.Dsh == "":
		out = append(out, "prepare: dsh "+cur+" — первая метка версии dsh-home")
	case c > 0:
		out = append(out, fmt.Sprintf("prepare: апгрейд dsh %s → %s (сессии и настройки dsh переносит сам)", st.Dsh, cur))
	case c < 0:
		out = append(out, fmt.Sprintf("prepare: ⚠ откат dsh %s → %s: сессии, записанные новой версией, эта может не открыть — новые версии пробуйте на клоне места («Клонировать»); бэкап dsh-home есть, только если образ меняли через agctl dev update", st.Dsh, cur))
	default:
		out = append(out, "prepare: dsh "+cur)
	}
	if st.Dsh != "" && st.Dsh != cur {
		st.History = append(st.History, stateChange{From: st.Dsh, To: cur, At: stamp})
		if len(st.History) > 10 {
			st.History = st.History[len(st.History)-10:]
		}
	}
	st.Dsh, st.Guest, st.Updated = cur, strings.TrimSpace(guest), stamp
	raw, _ := json.MarshalIndent(st, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err == nil {
		_ = os.Rename(tmp, path)
	} else {
		out = append(out, "prepare: ⚠ метка версии не записана: "+err.Error())
	}
	return out
}

func installedDshVersion(pkgPath string) string {
	raw, err := os.ReadFile(pkgPath) // #nosec G304 — путь из образа или флага оператора
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &pkg) != nil {
		return ""
	}
	return pkg.Version
}

// compareVersions — сравнение версий вида 1.2.3[-alpha.4]: >0 — a новее b.
// Пререлиз младше релиза той же тройки; пререлизы сравниваются по каналу
// (alpha < beta < rc) и номеру. Пустая b — a новее.
func compareVersions(a, b string) int {
	if b == "" {
		return 1
	}
	pa, pre1 := splitVersion(a)
	pb, pre2 := splitVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	switch {
	case pre1 == pre2:
		return 0
	case pre1 == "":
		return 1
	case pre2 == "":
		return -1
	}
	rank := func(p string) (int, int) {
		ch, num, _ := strings.Cut(p, ".")
		n, _ := strconv.Atoi(num)
		return map[string]int{"alpha": 1, "beta": 2, "rc": 3}[ch], n
	}
	c1, n1 := rank(pre1)
	c2, n2 := rank(pre2)
	if c1 != c2 {
		if c1 > c2 {
			return 1
		}
		return -1
	}
	switch {
	case n1 > n2:
		return 1
	case n1 < n2:
		return -1
	}
	return 0
}

func splitVersion(v string) ([3]int, string) {
	var out [3]int
	core, pre, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	for i, part := range strings.SplitN(core, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out, pre
}
