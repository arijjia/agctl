package server

// local_providers.go — провайдеры моделей в лаунчере (вкладка «Доступ»):
// провайдеры профиля организации и личные провайдеры разработчика одним
// списком, под своими именами. Личный провайдер хранится в настройках дома
// (providers:), его ключ — в хранилище секретов; в харнесс оба вида попадают
// одинаково — при старте рабочего места (provision.RefreshProvider).

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/provision"
)

// LocalProviderHooks — необязательная часть хоста лаунчера: личные
// провайдеры моделей. Хост без неё показывает только провайдеров профиля.
type LocalProviderHooks interface {
	Providers() []localmode.Provider
	// SaveProvider: key == nil — ключ не менять.
	SaveProvider(p localmode.Provider, key *string) error
	DeleteProvider(name string) error
	// ModelKey — значение ключа модели по имени секрета ("" — не задан).
	ModelKey(env string) string
}

func (s *Server) localProviderHooks() LocalProviderHooks {
	h, _ := s.localHooks().(LocalProviderHooks)
	return h
}

// syncLocalProviders — пересобрать дополнительных провайдеров сервера:
// провайдеры профиля (снимок на старте) + личные из настроек дома.
func (s *Server) syncLocalProviders() {
	h := s.localProviderHooks()
	if h == nil || s.local == nil {
		return
	}
	s.defaultLLMMu.Lock()
	defer s.defaultLLMMu.Unlock()
	extras := append([]provision.ExtraProvider(nil), s.local.profileExtras...)
	for i := range extras {
		if v := h.ModelKey(extras[i].KeyEnv()); v != "" {
			extras[i].APIKey = v
		}
	}
	for _, p := range h.Providers() {
		env := localmode.ProviderKeyEnv(p.Name)
		model := p.DefaultModel
		if model == "" && len(p.Models) > 0 {
			model = p.Models[0]
		}
		extras = append(extras, provision.ExtraProvider{
			Name: p.Name, BaseURL: p.BaseURL, Model: model, Models: p.Models,
			APIKeyEnv: env, APIKey: h.ModelKey(env),
		})
	}
	s.cfg.DefaultLLM.Extras = extras
}

// providerView — строка списка провайдеров в лаунчере.
type providerView struct {
	Name         string   `json:"name"`
	BaseURLs     []string `json:"baseUrls"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"defaultModel"`
	KeyEnv       string   `json:"keyEnv"`
	KeySet       bool     `json:"keySet"`
	// Source: profile — задан организацией (меняется только ключ);
	// own — личный провайдер разработчика (можно менять и удалять).
	Source  string `json:"source"`
	Primary bool   `json:"primary"`
}

func (s *Server) registerLocalProviderRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/ui/providers", s.requireLocalOwner(s.handleProvidersList))
	mux.HandleFunc("POST /api/local/ui/providers", s.requireLocalOwner(s.handleProviderSave))
	mux.HandleFunc("PUT /api/local/ui/providers/{name}", s.requireLocalOwner(s.handleProviderSave))
	mux.HandleFunc("DELETE /api/local/ui/providers/{name}", s.requireLocalOwner(s.handleProviderDelete))
}

func (s *Server) providerViews() []providerView {
	own := map[string]bool{}
	if h := s.localProviderHooks(); h != nil {
		for _, p := range h.Providers() {
			own[localmode.ProviderKeyEnv(p.Name)] = true
		}
	}
	p := s.defaultLLM()
	out := []providerView{}
	if p.BaseURL != "" || len(p.AllModels()) > 0 {
		out = append(out, providerView{Name: p.Alias(), BaseURLs: p.AllBaseURLs(), Models: p.AllModels(),
			DefaultModel: p.DefaultModel(), KeyEnv: "AGCTL_LLM_API_KEY", KeySet: p.APIKey != "", Source: "profile", Primary: true})
	}
	for _, e := range p.Extras {
		src := "profile"
		if own[e.KeyEnv()] {
			src = "own"
		}
		out = append(out, providerView{Name: e.Alias(), BaseURLs: []string{e.BaseURL}, Models: e.AllModels(),
			DefaultModel: e.DefaultModel(), KeyEnv: e.KeyEnv(), KeySet: e.APIKey != "", Source: src})
	}
	return out
}

func (s *Server) handleProvidersList(w http.ResponseWriter, r *http.Request, id identity) {
	// Провайдер или ключ, заданный в настройках харнесса, виден в лаунчере
	// при открытии вкладки — без перезапуска места.
	s.pullHarnessLLMAll()
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": s.providerViews(),
		"canAdd":    s.localProviderHooks() != nil,
	})
}

type providerReq struct {
	Name         string   `json:"name"`
	BaseURL      string   `json:"baseURL"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"defaultModel"`
	// Key: поле отсутствует — ключ не менять; пустая строка — убрать ключ.
	Key *string `json:"key"`
}

func (s *Server) handleProviderSave(w http.ResponseWriter, r *http.Request, id identity) {
	h := s.localProviderHooks()
	if h == nil {
		writeErr(w, http.StatusNotFound, "личные провайдеры недоступны на этом хосте")
		return
	}
	var req providerReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "ожидался JSON")
		return
	}
	updating := r.Method == http.MethodPut
	if updating {
		req.Name = r.PathValue("name")
	}
	p, err := localmode.NormalizeProvider(localmode.Provider{
		Name: req.Name, BaseURL: req.BaseURL, Models: req.Models, DefaultModel: req.DefaultModel})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	exists := false
	for _, v := range s.providerViews() {
		if v.Name != p.Name {
			continue
		}
		if v.Source != "own" {
			writeErr(w, http.StatusConflict, "провайдер «"+p.Name+"» задан профилем организации — выберите другое имя")
			return
		}
		exists = true
	}
	switch {
	case updating && !exists:
		writeErr(w, http.StatusNotFound, "провайдер «"+p.Name+"» не найден")
		return
	case !updating && exists:
		writeErr(w, http.StatusConflict, "провайдер «"+p.Name+"» уже есть")
		return
	}
	if err := h.SaveProvider(p, req.Key); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.afterLocalProvidersChange()
	_ = s.db.Audit(id.Login, "local.provider.save", p.Name, "")
	status := http.StatusOK
	if !updating {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"providers": s.providerViews()})
}

func (s *Server) handleProviderDelete(w http.ResponseWriter, r *http.Request, id identity) {
	h := s.localProviderHooks()
	if h == nil {
		writeErr(w, http.StatusNotFound, "личные провайдеры недоступны на этом хосте")
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if err := h.DeleteProvider(name); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	s.afterLocalProvidersChange()
	_ = s.db.Audit(id.Login, "local.provider.delete", name, "")
	writeJSON(w, http.StatusOK, map[string]any{"providers": s.providerViews()})
}

// afterLocalProvidersChange — новый состав провайдеров в сервер; работающие
// рабочие места получат его после перезапуска.
func (s *Server) afterLocalProvidersChange() {
	s.syncLocalProviders()
	s.pushLLMToPlaces()
	s.markLocalPlacesPending()
	s.local.events.publish("secrets")
	s.local.events.publish("places")
}

// markLocalPlacesPending — пометить работающие рабочие места владельца
// «нужен перезапуск»: доступы и провайдеры общие для всех мест.
func (s *Server) markLocalPlacesPending() {
	s.chatMu.Lock()
	var running []int
	for k := range s.chats {
		if k.uid == s.local.ownerID {
			running = append(running, k.slot)
		}
	}
	s.chatMu.Unlock()
	for _, slot := range running {
		s.placeTask(slot).markPending()
	}
}
