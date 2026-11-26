package server

// local_harness_sync.go — настройки моделей едут в обе стороны (локальный
// режим). Хост → харнесс: при старте места и сразу после правки в лаунчере
// (pushLLMToPlaces). Харнесс → хост: правки из Settings → Models dsh
// переносятся в настройки дома и хранилище секретов (pullHarnessLLM) —
// перед каждой записью в dsh-home и при открытии вкладки «Доступ».
// Правила переноса:
//   - провайдер, добавленный в харнессе, становится личным провайдером;
//   - у личного провайдера переносятся адрес, модели, ключ и удаление;
//   - у провайдера организации переносится только ключ: его состав задаёт
//     профиль, правки состава из харнесса не сохраняются.

import (
	"log"
	"os"
	"strings"

	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/provision"
)

// pullHarnessLLM переносит на хост правки моделей, сделанные в харнессе
// одного рабочего места. true — на хосте что-то изменилось.
func (s *Server) pullHarnessLLM(dshHome string) bool {
	h := s.localProviderHooks()
	if h == nil || s.local == nil {
		return false
	}
	changes, err := provision.DiffHarnessLLM(dshHome)
	if err != nil {
		log.Printf("[sync] %s: %v", dshHome, err)
		return false
	}
	if len(changes) == 0 {
		return false
	}
	own := map[string]localmode.Provider{}
	for _, p := range h.Providers() {
		own[p.Name] = p
	}
	profileKeyEnv := map[string]string{} // имя провайдера организации → имя его ключа
	def := s.defaultLLM()
	profileKeyEnv[def.Alias()] = "AGCTL_LLM_API_KEY"
	for _, e := range s.local.profileExtras {
		profileKeyEnv[e.Alias()] = e.KeyEnv()
	}
	applied := false
	for _, ch := range changes {
		name := ch.Provider.Name
		keyEnv, isProfile := profileKeyEnv[name]
		_, isOwn := own[name]
		switch {
		case isProfile:
			if ch.Kind == "changed" && ch.KeyChanged && ch.Provider.Key != "" {
				if err := s.localHooks().SetValue(keyEnv, ch.Provider.Key, false); err != nil {
					log.Printf("[sync] ключ %s из харнесса: %v", name, err)
					continue
				}
				log.Printf("[sync] ключ провайдера %s обновлён из харнесса", name)
				applied = true
			}
		case ch.Kind == "removed":
			if isOwn {
				if err := h.DeleteProvider(name); err == nil {
					log.Printf("[sync] провайдер %s удалён в харнессе — убран из настроек дома", name)
					applied = true
				}
			}
		default: // added | changed — личный провайдер
			// Имя из харнесса приводится к правилам имён провайдеров дома.
			p := localmode.Provider{Name: harnessProviderName(name), BaseURL: ch.Provider.BaseURL, Models: ch.Provider.Models}
			if prev, ok := own[name]; ok && containsString(p.Models, prev.DefaultModel) {
				p.DefaultModel = prev.DefaultModel
			}
			var key *string
			if ch.KeyChanged {
				k := ch.Provider.Key
				key = &k
			}
			if err := h.SaveProvider(p, key); err != nil {
				log.Printf("[sync] провайдер %s из харнесса не перенесён: %v", name, err)
				continue
			}
			log.Printf("[sync] провайдер %s перенесён из харнесса в настройки дома", name)
			applied = true
		}
	}
	// Перенесённое (и сознательно пропущенное) больше правкой не считается.
	if err := provision.WriteLLMBaseline(dshHome); err != nil {
		log.Printf("[sync] %s: снимок: %v", dshHome, err)
	}
	if applied {
		if key := s.localHooks().LLMKey(); key != "" {
			s.setDefaultLLMKey(key)
		}
		s.syncLocalProviders()
	}
	return applied
}

// ownerDshHomes — dsh-home мест владельца: основное и все работающие.
func (s *Server) ownerDshHomes() []string {
	keys := map[chatKey]bool{{uid: s.local.ownerID, slot: 0}: true}
	s.chatMu.Lock()
	for k := range s.chats {
		if k.uid == s.local.ownerID {
			keys[k] = true
		}
	}
	s.chatMu.Unlock()
	var out []string
	for k := range keys {
		home := s.dshHomeFor(k)
		if _, err := os.Stat(home); err == nil {
			out = append(out, home)
		}
	}
	return out
}

// pullHarnessLLMAll — правки харнесса со всех мест владельца; true — на
// хосте что-то изменилось.
func (s *Server) pullHarnessLLMAll() bool {
	changed := false
	for _, home := range s.ownerDshHomes() {
		changed = s.pullHarnessLLM(home) || changed
	}
	return changed
}

// pushLLMToPlaces — записать текущих провайдеров и ключи в dsh-home мест
// владельца. Ключи харнесс подхватывает сразу (.credentials.yaml он
// перечитывает сам), состав провайдеров — после перезапуска места.
func (s *Server) pushLLMToPlaces() {
	s.pullHarnessLLMAll() // сначала забрать правки харнесса, чтобы их не затереть
	p := s.quotaAwareProvider(s.local.ownerID)
	for _, home := range s.ownerDshHomes() {
		if err := provision.RefreshProvider(home, p); err != nil {
			log.Printf("[sync] %s: %v", home, err)
		}
	}
}

// harnessProviderName — имя провайдера из харнесса в виде, допустимом для
// настроек дома: строчная латиница, цифры, дефис, точка.
func harnessProviderName(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if len(out) > 40 {
		out = out[:40]
	}
	if out == "" {
		out = "provider"
	}
	return out
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
