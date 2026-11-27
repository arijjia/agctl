package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/backlog"
	"github.com/arijjia/agctl/internal/cli"
)

const (
	mR = cli.MarkRead
	mW = cli.MarkWrite
	mE = cli.MarkEffect
)

// agctlMarks — канон меток дерева команд agctl (cli-std): 🔍 читающая,
// ✏️ мутирующая, ⚠️ с побочным эффектом (демон, контейнер, браузер, сеть).
// Новая подкоманда — сюда, в справку и в комплишены: тест сверяет все три.
var agctlMarks = cli.CommandMarks{
	"version": mR, "help": mR, "completion fish": mR,

	"backlog new": mW, "backlog init": mW, "backlog list": mR, "backlog top": mR, "backlog check": mR,
	"backlog start": mW, "backlog done": mW, "backlog cancel": mW, "backlog note": mW, "backlog migrate": mW,

	"dev init": mW, "dev up": mE, "dev down": mE, "dev status": mR, "dev paths": mR, "dev images": mR,
	"dev llm-check": mE, "dev open": mE, "dev logs": mR, "dev update": mW, "dev rollback": mW,
	"dev doctor": mE, "dev migrate": mW, "dev help": mR,

	"dev certs list": mR, "dev certs host": mW, "dev certs fetch": mW, "dev certs add": mW,
	"dev certs rm": mW, "dev certs check": mE,

	"dev secrets list": mR, "dev secrets set": mW, "dev secrets rm": mW,

	"dev bridge list": mR, "dev bridge pending": mR, "dev bridge approve": mW, "dev bridge deny": mW,
	"dev bridge log": mR,
}

// Метки инвазивности (cli-std) в комплишенах и справке совпадают с каноном,
// и каждая подкоманда канона есть в комплишенах.
func TestCliStdMarks(t *testing.T) {
	seen, problems := cli.CheckFishMarks(fishCompletionScript, agctlMarks, map[string]string{
		"__fish_use_subcommand": "", "__agctl_backlog_needs_sub": "backlog", "__agctl_at dev": "dev",
		"__agctl_at dev certs": "dev certs", "__agctl_at dev secrets": "dev secrets",
		"__agctl_at dev bridge": "dev bridge", "__agctl_at completion": "completion",
	})
	for _, leaf := range agctlMarks.Leaves() {
		if !seen[leaf] {
			problems = append(problems, "в комплишенах нет подкоманды "+leaf)
		}
	}
	problems = append(problems, cli.CheckUsageMarks(devUsage, "dev", agctlMarks)...)
	problems = append(problems, cli.CheckUsageMarks(devCertsUsage, "dev certs", agctlMarks)...)
	var out, errb bytes.Buffer
	backlog.Main([]string{"help"}, &out, &errb)
	problems = append(problems, cli.CheckUsageMarks(out.String()+errb.String(), "backlog", agctlMarks, "init")...)

	// верхний уровень: строки-листья — с меткой, сводные строки — пары «метка имя» по канону
	for _, row := range help.CommandRows() {
		name := strings.Fields(row.Name)
		path := strings.Join(name, " ")
		if len(name) > 1 && strings.HasPrefix(name[1], "[") {
			path = name[0]
		}
		if path == "dev up" || strings.HasPrefix(row.Name, "dev up") {
			path = "dev up"
		}
		mark, ok := agctlMarks.Mark(path)
		if !ok {
			problems = append(problems, "в каноне нет "+path+" (строка справки "+row.Name+")")
			continue
		}
		if !cli.HasMark(row.Desc[0], mark) {
			problems = append(problems, "agctl help: "+path+": ждали «"+mark+"»: "+row.Desc[0])
		}
		for _, d := range row.Desc[1:] {
			problems = append(problems, cli.CheckMarkPairs(d, "dev", agctlMarks)...)
		}
		if path == "backlog" {
			problems = append(problems, cli.CheckMarkPairs(row.Desc[0], "backlog", agctlMarks)...)
		}
	}
	for _, p := range problems {
		t.Error(p)
	}
}
