package axrt

// egress.go — автоматизация egress-инъекции ключей LLM (режим
// InjectLLMKey=false): agctl сам создаёт/обновляет EgressPolicy актора через
// Control API Substrate (CreateActorEgressPolicy/UpdateActorEgressPolicy) и
// держит ключи провайдеров в k8s Secret своего namespace. Песочница dsh
// отправляет `Authorization: Bearer placeholder`, шлюз atenet-egress
// подменяет заголовок реальным ключом по credentialUri
// `ate-secret://k8s.io/cluster/<cluster>/<namespace>/<secret>/token` — ключ
// не попадает ни в env задачи, ни в снимок актора, ни в `ax get task`.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// PlaceholderAPIKey — значение ключа в env задачи при egress-инъекции:
// dsh отправляет его как Bearer-токен, шлюз подменяет реальным ключом.
const PlaceholderAPIKey = "placeholder"

// egressPolicyName — синглтон-имя политики актора (контракт Substrate).
const egressPolicyName = "default"

// SecretKeyToken — ключ Secret с токеном провайдера.
const SecretKeyToken = "token"

// ControlClient — подмножество Control API Substrate для EgressPolicy.
type ControlClient interface {
	GetActorEgressPolicy(ctx context.Context, req *ateapi.GetActorEgressPolicyRequest, opts ...grpc.CallOption) (*ateapi.EgressPolicy, error)
	CreateActorEgressPolicy(ctx context.Context, req *ateapi.CreateActorEgressPolicyRequest, opts ...grpc.CallOption) (*ateapi.EgressPolicy, error)
	UpdateActorEgressPolicy(ctx context.Context, req *ateapi.UpdateActorEgressPolicyRequest, opts ...grpc.CallOption) (*ateapi.EgressPolicy, error)
}

// SecretWriter — sync непрозрачного Secret с ключами провайдеров
// (реализация — k8s client-go поверх in-cluster credentials).
type SecretWriter interface {
	// EnsureSecret создаёт Secret при отсутствии и обновляет data при
	// расхождении; idempotent.
	EnsureSecret(ctx context.Context, namespace, name string, data map[string][]byte) error
}

// EgressAutomation — параметры автоматической egress-инъекции.
type EgressAutomation struct {
	// Control — клиент Control API Substrate (ate-api-server).
	Control ControlClient
	// Secrets — писатель k8s Secret в namespace шлюза agctl.
	Secrets SecretWriter
	// Cluster — имя кластера в credentialUri (ate-secret://k8s.io/cluster/<cluster>/…).
	Cluster string
	// Namespace — namespace Secret'ов agctl (namespace пода шлюза).
	Namespace string
}

// egressEndpoint — endpoint провайдера: host + scheme + секрет с ключом.
type egressEndpoint struct {
	host   string
	scheme string
	secret string
	cred   string
}

// egressEndpoints — правила по всем эндпоинтам провайдеров с ключами:
// основной провайдер (все его BaseURLs) + extras. Возврат отсортирован по
// host для стабильного сравнения правил; дубликаты host схлопываются.
func (e *EgressAutomation) egressEndpoints(spec agctlrt.ChatSpec) ([]egressEndpoint, error) {
	type providerEndpoints struct {
		alias string
		key   string
		urls  []string
	}
	provs := []providerEndpoints{}
	p := spec.Provider
	if p.APIKey != "" {
		provs = append(provs, providerEndpoints{alias: p.Alias(), key: p.APIKey, urls: p.AllBaseURLs()})
	}
	for _, extra := range p.Extras {
		if extra.APIKey == "" || strings.TrimSpace(extra.BaseURL) == "" {
			continue
		}
		provs = append(provs, providerEndpoints{alias: extra.Name, key: extra.APIKey, urls: []string{extra.BaseURL}})
	}
	byHost := map[string]egressEndpoint{}
	secrets := map[string]string{} // secret → key
	for _, prov := range provs {
		secret := secretNameFor(prov.alias)
		if prev, seen := secrets[secret]; seen && prev != prov.key {
			return nil, fmt.Errorf("egress: alias collision on Secret %s", secret)
		}
		secrets[secret] = prov.key
		for _, raw := range prov.urls {
			host, scheme, err := egressHost(raw)
			if err != nil {
				return nil, fmt.Errorf("egress: provider %q: %w", prov.alias, err)
			}
			if host == "" {
				continue // IP/безымянный хост — вручную оператором
			}
			if _, dup := byHost[host]; dup {
				continue
			}
			byHost[host] = egressEndpoint{
				host:   host,
				scheme: scheme,
				secret: secret,
				cred:   fmt.Sprintf("ate-secret://k8s.io/cluster/%s/%s/%s/%s", e.Cluster, e.Namespace, secret, SecretKeyToken),
			}
		}
	}
	hosts := make([]string, 0, len(byHost))
	for h := range byHost {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	out := make([]egressEndpoint, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, byHost[h])
	}
	return out, nil
}

// secretData — Secret'ы с ключами (по одному на алиас провайдера).
func (e *EgressAutomation) secretData(spec agctlrt.ChatSpec) (map[string]map[string][]byte, error) {
	data := map[string]map[string][]byte{}
	add := func(alias, key string) error {
		if key == "" {
			return nil
		}
		secret := secretNameFor(alias)
		if data[secret] == nil {
			data[secret] = map[string][]byte{}
		}
		if prev, seen := data[secret][SecretKeyToken]; seen && string(prev) != key {
			return fmt.Errorf("egress: alias collision on Secret %s", secret)
		}
		data[secret][SecretKeyToken] = []byte(key)
		return nil
	}
	p := spec.Provider
	if err := add(p.Alias(), p.APIKey); err != nil {
		return nil, err
	}
	for _, extra := range p.Extras {
		if err := add(extra.Name, extra.APIKey); err != nil {
			return nil, err
		}
	}
	return data, nil
}

// EnsureEgressPolicy — политика актора <actor> соответствует ключам
// провайдеров spec: Secret'ы с ключами синхронизированы, правила
// replaceHeaders Authorization ведут на них. Idempotent: без расхождения
// ничего не пишет.
func (e *EgressAutomation) EnsureEgressPolicy(ctx context.Context, atespace, actor string, spec agctlrt.ChatSpec) error {
	if e == nil || e.Control == nil || e.Secrets == nil {
		return fmt.Errorf("axrt: egress automation is not configured")
	}
	if e.Cluster == "" || e.Namespace == "" {
		return fmt.Errorf("axrt: egress automation requires cluster and namespace")
	}

	// 1. Secret'ы с ключами провайдеров.
	data, err := e.secretData(spec)
	if err != nil {
		return fmt.Errorf("axrt: %w", err)
	}
	names := make([]string, 0, len(data))
	for n := range data {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := e.Secrets.EnsureSecret(ctx, e.Namespace, n, data[n]); err != nil {
			return fmt.Errorf("axrt: ensure Secret %s/%s: %w", e.Namespace, n, err)
		}
	}

	// 2. Политика актора.
	eps, err := e.egressEndpoints(spec)
	if err != nil {
		return fmt.Errorf("axrt: %w", err)
	}
	var rules []*ateapi.EgressRule
	for _, ep := range eps {
		effect := &ateapi.HttpRuleEffects{ReplaceHeaders: []*ateapi.CredentialHeader{{
			Header: "Authorization", Prefix: "Bearer ", CredentialUri: ep.cred,
		}}}
		if ep.scheme == "http" {
			rules = append(rules, &ateapi.EgressRule{Http: &ateapi.HttpRule{Hostnames: []string{ep.host}, Effects: effect}})
		} else {
			rules = append(rules, &ateapi.EgressRule{Https: &ateapi.HttpsRule{Hostnames: []string{ep.host}, Effects: effect}})
		}
	}
	actorRef := &ateapi.ObjectRef{Atespace: atespace, Name: actor}
	existing, err := e.Control.GetActorEgressPolicy(ctx, &ateapi.GetActorEgressPolicyRequest{Actor: actorRef, Name: egressPolicyName})
	if status.Code(err) == codes.NotFound {
		_, err := e.Control.CreateActorEgressPolicy(ctx, &ateapi.CreateActorEgressPolicyRequest{
			Actor: actorRef,
			EgressPolicy: &ateapi.EgressPolicy{
				Metadata: &ateapi.ResourceMetadata{Atespace: atespace, Name: egressPolicyName},
				Rules:    rules,
			},
		})
		if err == nil {
			return nil
		}
		if status.Code(err) != codes.AlreadyExists {
			return fmt.Errorf("axrt: create egress policy for %s: %w", actor, err)
		}
		// гонка создания — падаем в ветку обновления
		existing, err = e.Control.GetActorEgressPolicy(ctx, &ateapi.GetActorEgressPolicyRequest{Actor: actorRef, Name: egressPolicyName})
	}
	if err != nil {
		return fmt.Errorf("axrt: get egress policy for %s: %w", actor, err)
	}
	if rulesEqual(existing.GetRules(), rules) {
		return nil
	}
	updated := proto.Clone(existing).(*ateapi.EgressPolicy)
	updated.Rules = rules
	if _, err := e.Control.UpdateActorEgressPolicy(ctx, &ateapi.UpdateActorEgressPolicyRequest{Actor: actorRef, EgressPolicy: updated}); err != nil {
		return fmt.Errorf("axrt: update egress policy for %s: %w", actor, err)
	}
	return nil
}

// rulesEqual — равенство правил (порядок зафиксирован сортировкой host).
func rulesEqual(a, b []*ateapi.EgressRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !proto.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// secretNameFor — детерминированное имя Secret по алиасу провайдера:
// agctl-llm-<8 hex sha256>; короткое, без пользовательских данных.
func secretNameFor(alias string) string {
	sum := sha256.Sum256([]byte(alias))
	return "agctl-llm-" + hex.EncodeToString(sum[:4])
}

// egressHost — hostname и scheme из base URL провайдера; IP-адреса и
// не-DNS имена паттернами Substrate не поддерживаются (пустой host =
// правило не строится, endpoint остаётся за ручной политикой оператора).
func egressHost(baseURL string) (host, scheme string, err error) {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return "", "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("base URL %q: %w", baseURL, err)
	}
	host = strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || net.ParseIP(host) != nil {
		return "", "", nil
	}
	scheme = strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		scheme = "https"
	}
	return host, scheme, nil
}
