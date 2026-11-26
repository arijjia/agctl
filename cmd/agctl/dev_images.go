package main

// dev_images.go — `agctl dev images`: каталог образов дома (профиль +
// текущий) и что в каждом: есть ли на машине, версия dsh, сборка guest,
// дата и размер, не устарел ли образ для этой сборки agctl. То же, что
// лаунчер показывает в «Рабочих местах», — для терминала и скриптов.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/arijjia/agctl/internal/localmode"
)

func devImages(home string, args []string) error {
	fs := flag.NewFlagSet("dev images", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "машиночитаемый вывод")
	_ = fs.Parse(args)
	cfg, err := localmode.Load(home)
	if err != nil {
		return err
	}
	p, _, err := localmode.ResolveProfile(cfg)
	if err != nil {
		return err
	}
	h := &devHooks{home: home, prof: p}
	images := h.Images()
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"current": h.CurrentImage(), "images": images})
	}
	current := h.CurrentImage()
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ОБРАЗ\tНА МАШИНЕ\tDSH\tGUEST\tСОБРАН\tРАЗМЕР\t")
	for _, im := range images {
		ref := im.Ref
		if ref == current {
			ref += " *"
		}
		here, size := "нет", "—"
		if im.Present {
			here = "есть"
			size = fmt.Sprintf("%.1f ГБ", float64(im.SizeMB)/1024)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t\n", ref, here, orQ(im.DshVersion), orQ(im.GuestRevision), orQ(im.Built), size)
	}
	_ = tw.Flush()
	fmt.Println("* — образ основного места. Тег вида 0.2.3-alpha.2 — версия dsh: такой образ соберётся на этой версии.")
	for _, im := range images {
		if im.Outdated && im.Present {
			fmt.Printf("⚠ %s: %s\n", im.Ref, im.Note)
		}
	}
	return nil
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
