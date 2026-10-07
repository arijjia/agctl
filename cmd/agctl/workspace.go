// agctl workspace — управление рабочими местами из CLI (без веб-панели ЛК).
// Сейчас: только create (синхронный аналог server.provisionJob).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/store"
)

var dshTokenRe = regexp.MustCompile(`[?&]token=([A-Za-z0-9_\-]+)`)

func cmdWorkspace(args []string) {
	if len(args) < 1 {
		fmt.Fprint(os.Stderr, "использование: agctl workspace create [флаги]\n")
		os.Exit(2)
	}
	switch args[0] {
	case "create":
		cmdWorkspaceCreate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "неизвестная подкоманда workspace %q\n", args[0])
		os.Exit(2)
	}
}

func cmdWorkspaceCreate(args []string) {
	fsSet := flag.NewFlagSet("workspace create", flag.ExitOnError)
	home := fsSet.String("home", defaultHome(), "каталог данных agctl")
	userLogin := fsSet.String("user", "", "логин владельца (должен существовать: регистрация через ЛК)")
	name := fsSet.String("name", "", "имя рабочего места (минимум 2 символа)")
	template := fsSet.String("template", "1c-vibe", "шаблон: base | 1c-vibe")
	baseURL := fsSet.String("base-url", "", "Base URL LLM-провайдера (пусто = из сохранённого секрета)")
	model := fsSet.String("model", "", "модель по умолчанию (пусто = из сохранённого секрета)")
	models := fsSet.String("models", "", "модели через запятую (пусто = из сохранённого секрета)")
	apiKey := fsSet.String("api-key", os.Getenv("AGCTL_API_KEY"), "токен провайдера (или env AGCTL_API_KEY; пусто = из сохранённого секрета)")
	image := fsSet.String("image", "agctl-dsh:0.1.0", "тег образа agctl-dsh")
	binDir := fsSet.String("bin-dir", "", "каталог linux-сборок agctl для образа (по умолчанию ./dist)")
	maxPerUser := fsSet.Int("max-per-user", 3, "лимит воркспейсов на пользователя")
	memGB := fsSet.Float64("mem-gb", 2, "лимит памяти контейнера, ГБ")
	cpus := fsSet.Float64("cpus", 2, "лимит CPU контейнера")
	waitSec := fsSet.Int("wait", 45, "ждать boot-токен dsh, секунд (0 = не ждать)")
	_ = fsSet.Parse(args)

	if strings.TrimSpace(*userLogin) == "" {
		log.Fatal("нужен --user (пользователь создаётся регистрацией в ЛК: agctl serve)")
	}
	if len(strings.TrimSpace(*name)) < 2 {
		log.Fatal("имя воркспейса: минимум 2 символа (--name)")
	}
	if !provision.ValidTemplate(*template) {
		log.Fatalf("неизвестный шаблон %q (base | 1c-vibe)", *template)
	}

	absHome, err := requireHome(*home)
	if err != nil {
		log.Fatal(err)
	}
	keyPath, err := secrets.EnsureMasterKey(absHome)
	if err != nil {
		log.Fatal(err)
	}
	sec, err := secrets.New(keyPath)
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(filepath.Join(absHome, "agctl.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	owner, err := db.UserByLogin(strings.TrimSpace(*userLogin))
	if err != nil {
		log.Fatalf("пользователь %q не найден: сначала зарегистрируйтесь в ЛК (agctl serve)", *userLogin)
	}
	own, err := db.WorkspacesByOwner(owner.ID)
	if err != nil {
		log.Fatal(err)
	}
	if len(own) >= *maxPerUser {
		log.Fatalf("достигнут лимит воркспейсов (%d)", *maxPerUser)
	}

	// провайдер: явные флаги, недостающее — из сохранённого секрета
	p, err := cliProvider(db, sec, owner.ID, *baseURL, *model, *models, *apiKey)
	if err != nil {
		log.Fatal(err)
	}

	wsID, err := provision.NewID()
	if err != nil {
		log.Fatal(err)
	}
	port, err := dockerrt.FreePort()
	if err != nil {
		log.Fatal("нет свободного порта: ", err)
	}
	paths := provision.Paths(absHome, owner.Login, wsID, *name)
	ws := store.Workspace{
		ID: wsID, OwnerID: owner.ID, Name: strings.TrimSpace(*name), Template: *template,
		Status: "provisioning", HostPort: port, BaseURL: p.BaseURL,
		Model: p.DefaultModel(), Models: p.AllModels(),
	}
	if err := db.InsertWorkspace(ws); err != nil {
		log.Fatal(err)
	}
	fail := func(stage string, err error) {
		_ = db.UpdateWorkspaceStatus(ws.ID, "failed", stage+": "+err.Error())
		_ = db.Audit(owner.Login, "workspace.fail", ws.ID, stage+": "+err.Error())
		log.Fatalf("%s: %v", stage, err)
	}
	_ = db.Audit(owner.Login, "workspace.create", wsID, "name="+ws.Name+" cli=1")

	cipher, err := sec.Encrypt(p.APIKey)
	if err != nil {
		fail("секрет", err)
	}
	if err := db.SecretUpsert(owner.ID, p.BaseURL, p.DefaultModel(), p.AllModels(), cipher); err != nil {
		fail("секрет", err)
	}

	log.Printf("provision: %s (шаблон %s)…", wsID, *template)
	if err := provision.Provision(paths, p, *template, assets.RepoTemplate, assets.DshSkel); err != nil {
		fail("provision", err)
	}
	if err := db.UpdateWorkspacePaths(ws.ID, paths.Repo, paths.Work, paths.DshHome); err != nil {
		fail("db", err)
	}

	rt, err := dockerrt.New(dockerrt.Config{
		Image:       *image,
		MemoryBytes: int64(*memGB * 1024 * 1024 * 1024),
		NanoCPUs:    int64(*cpus * 1e9),
		BinDir:      firstNonEmpty(*binDir, "dist"),
	}, assets.Image)
	if err != nil {
		fail("docker", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	log.Printf("docker: запуск контейнера…")
	res, err := rt.Start(ctx, paths, ws.ID, ws.HostPort, p)
	if err != nil {
		fail("docker", err)
	}
	if err := db.UpdateWorkspaceRuntime(ws.ID, res.ContainerID, res.HostPort, "running"); err != nil {
		fail("db", err)
	}
	_ = db.Audit(owner.Login, "workspace.running", ws.ID, fmt.Sprintf("port=%d cli=1", res.HostPort))

	url := fmt.Sprintf("http://127.0.0.1:%d/", res.HostPort)
	if *waitSec > 0 {
		if tok := waitToken(ctx, rt, res.ContainerID, *waitSec); tok != "" {
			url += "?token=" + tok
		}
	}
	fmt.Printf("рабочее место %s (%s) — running\n  dsh web: %s\n  репо: %s\n",
		ws.ID, ws.Name, url, paths.Work)
}

// cliProvider — как server.providerOf: явные значения, иначе сохранённый секрет.
func cliProvider(db *store.DB, sec *secrets.Manager, ownerID int64, baseURL, model, models, apiKey string) (provision.Provider, error) {
	ms := provision.ParseModels(models, model)
	if apiKey == "" {
		s, err := db.SecretByUser(ownerID)
		if err != nil {
			return provision.Provider{}, fmt.Errorf("нет сохранённого токена — укажите --api-key (или AGCTL_API_KEY)")
		}
		key, err := sec.Decrypt(s.Ciphertext)
		if err != nil {
			return provision.Provider{}, fmt.Errorf("токен не расшифровывается: %w", err)
		}
		base, got := s.BaseURL, s.Models
		if baseURL != "" {
			base = baseURL
		}
		if len(ms) > 0 {
			got = ms
		}
		if !strings.Contains(base, "://") {
			return provision.Provider{}, fmt.Errorf("Base URL должен начинаться с http(s)://")
		}
		if len(got) == 0 {
			return provision.Provider{}, fmt.Errorf("укажите хотя бы одну модель (--model/--models)")
		}
		return provision.Provider{BaseURL: base, Model: got[0], Models: got, APIKey: key}, nil
	}
	if !strings.Contains(baseURL, "://") {
		return provision.Provider{}, fmt.Errorf("Base URL должен начинаться с http(s):// (--base-url)")
	}
	if len(ms) == 0 {
		return provision.Provider{}, fmt.Errorf("укажите хотя бы одну модель (--model/--models)")
	}
	return provision.Provider{BaseURL: baseURL, Model: ms[0], Models: ms, APIKey: apiKey}, nil
}

// waitToken — boot-токен dsh из журнала контейнера (как server.wsToken).
func waitToken(ctx context.Context, rt *dockerrt.Runtime, containerID string, waitSec int) string {
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	for time.Now().Before(deadline) {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		logs, err := rt.Logs(cctx, containerID, "80")
		cancel()
		if err == nil {
			if m := dshTokenRe.FindStringSubmatch(logs); m != nil {
				return m[1]
			}
		}
		time.Sleep(2 * time.Second)
	}
	return ""
}
