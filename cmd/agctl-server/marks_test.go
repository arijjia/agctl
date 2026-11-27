package main

import (
	"testing"

	"github.com/arijjia/agctl/internal/cli"
)

// serverMarks — канон меток дерева команд agctl-server (cli-std).
var serverMarks = cli.CommandMarks{
	"serve": cli.MarkEffect, "migrate": cli.MarkWrite, "seed-demo-users": cli.MarkWrite,
	"connect": cli.MarkEffect, "config": cli.MarkRead, "version": cli.MarkRead, "help": cli.MarkRead,
	"completion fish": cli.MarkRead,
	"sync create":     cli.MarkWrite, "sync list": cli.MarkRead, "sync pause": cli.MarkEffect,
	"sync resume": cli.MarkEffect, "sync flush": cli.MarkWrite, "sync terminate": cli.MarkWrite,
}

// Метки инвазивности (cli-std) в комплишенах и справке agctl-server.
func TestCliStdMarks(t *testing.T) {
	seen, problems := cli.CheckFishMarks(fishCompletionScript, serverMarks, map[string]string{
		"__fish_use_subcommand": "", "__agctl_server_sync_needs_sub": "sync", "__agctl_server_at completion": "completion",
	})
	for _, leaf := range serverMarks.Leaves() {
		if !seen[leaf] {
			problems = append(problems, "в комплишенах нет подкоманды "+leaf)
		}
	}
	problems = append(problems, cli.CheckUsageMarks(syncUsage, "sync", serverMarks)...)
	for _, row := range help.CommandRows() {
		mark, ok := serverMarks.Mark(row.Name)
		if !ok {
			problems = append(problems, "в каноне нет "+row.Name)
			continue
		}
		if !cli.HasMark(row.Desc[0], mark) {
			problems = append(problems, "agctl-server help: "+row.Name+": ждали «"+mark+"»: "+row.Desc[0])
		}
		for _, d := range row.Desc {
			problems = append(problems, cli.CheckMarkPairs(d, row.Name, serverMarks)...)
		}
	}
	for _, p := range problems {
		t.Error(p)
	}
}
