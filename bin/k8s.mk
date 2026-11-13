# ── k8s: образы, деплой и смоук agctl в Kubernetes (docs/kubernetes.md) ──────
# REGISTRY — реестр, доступный кластеру (registry.example.com/agctl).
# Образы: agctl-serve (шлюз, deploy/serve.Dockerfile) и agctl-dsh/-nossh
# (рабочее место; те же, что img_build). Для бэкенда ax нужен digest agctl-dsh.
REGISTRY ?=
K8S_OVERLAY ?= base
K8S_NAMESPACE ?= agctl
K8S_ARCH ?= amd64
IMG_SERVE ?= agctl-serve:$(VERSION)

.PHONY: img_serve_build img_push k8s_render k8s_apply k8s_profile k8s_secret k8s_status k8s_e2e_kind

## img_serve_build: образ шлюза agctl-serve (linux/$(K8S_ARCH), distroless)
img_serve_build:
	GOOS=linux GOARCH=$(K8S_ARCH) CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-linux-$(K8S_ARCH) ./cmd/agctl
	docker build --platform linux/$(K8S_ARCH) --build-arg TARGETARCH=$(K8S_ARCH) -t $(IMG_SERVE) -f deploy/serve.Dockerfile .

## img_push: тегировать и запушить agctl-serve, agctl-dsh, agctl-dsh-nossh, вкусы (web, вкусы профиля) и надстройки профиля PROFILE в REGISTRY (Nexus и пр.); печатает digest agctl-dsh (для AGCTL_AX_IMAGE)
img_push:
	@test -n "$(REGISTRY)" || { echo "✗ задай REGISTRY=<реестр>/agctl"; exit 1; }
	@set -e; for img in $(IMG_SERVE) $(IMG) $(IMG_NOSSH) $(if $(PROFILE_DOCKERFILE),$(call overlay_tag,$(IMG)) $(call overlay_tag,$(IMG_NOSSH))); do \
	  docker tag "$$img" "$(REGISTRY)/$$img"; docker push "$(REGISTRY)/$$img"; done
	@# Образы локального режима (вкусы): пушим те, что собраны.
	@set -e; for img in $(IMG_WEB) $(if $(PROFILE_DOCKERFILE),$(call overlay_tag,$(IMG_WEB))) $(foreach fl,$(PROFILE_FLAVORS),$(call flavor_tag,$(fl))); do \
	  if docker image inspect "$$img" >/dev/null 2>&1; then docker tag "$$img" "$(REGISTRY)/$$img"; docker push "$(REGISTRY)/$$img"; fi; done
	@echo "digest agctl-dsh (AGCTL_AX_IMAGE):"; \
	docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$(REGISTRY)/$(IMG)" | grep "^$(REGISTRY)/$(firstword $(subst :, ,$(IMG)))@" | head -n1

## k8s_render: отрендерить манифесты (kustomize overlay K8S_OVERLAY: base | overlays/kind | overlays/ax)
k8s_render:
	kubectl kustomize deploy/k8s/$(K8S_OVERLAY)

## k8s_apply: применить манифесты шлюза (K8S_OVERLAY) и дождаться rollout
k8s_apply:
	kubectl apply -k deploy/k8s/$(K8S_OVERLAY)
	kubectl -n $(K8S_NAMESPACE) rollout status deployment/agctl --timeout=180s

## k8s_profile: профиль организации в кластер (ConfigMap profile.yaml + assets) из profiles/$(PROFILE)
k8s_profile:
	@test -f "profiles/$(PROFILE)/profile.yaml" || { echo "✗ profiles/$(PROFILE)/profile.yaml не найден (make profile-fetch …)"; exit 1; }
	kubectl -n $(K8S_NAMESPACE) create configmap agctl-profile --from-file=profile.yaml=profiles/$(PROFILE)/profile.yaml --dry-run=client -o yaml | kubectl apply -f -
	@if [ -d "profiles/$(PROFILE)/assets" ]; then \
	  kubectl -n $(K8S_NAMESPACE) create configmap agctl-profile-assets --from-file=profiles/$(PROFILE)/assets --dry-run=client -o yaml | kubectl apply -f -; fi
	kubectl -n $(K8S_NAMESPACE) rollout restart deployment/agctl 2>/dev/null || true

## k8s_secret: ключи LLM из .env в Secret agctl-llm (AGCTL_LLM_API_KEY, AGCTL_LLM_CORP_API_KEY)
k8s_secret:
	@test -n "$(AGCTL_LLM_API_KEY)" || echo "⚠ AGCTL_LLM_API_KEY пуст (.env) — секрет будет без ключа основного провайдера"
	kubectl -n $(K8S_NAMESPACE) create secret generic agctl-llm \
	  --from-literal=AGCTL_LLM_API_KEY="$(AGCTL_LLM_API_KEY)" \
	  --from-literal=AGCTL_LLM_CORP_API_KEY="$(AGCTL_LLM_CORP_API_KEY)" \
	  --dry-run=client -o yaml | kubectl apply -f -

## k8s_status: шлюз, поды пользователей и их PVC
k8s_status:
	kubectl -n $(K8S_NAMESPACE) get deploy,pod,svc,pvc,networkpolicy -l app.kubernetes.io/managed-by=agctl -o wide 2>/dev/null || true
	kubectl -n $(K8S_NAMESPACE) get deploy/agctl pod -l app.kubernetes.io/name=agctl -o wide

## k8s_e2e_kind: смоук бэкенда k8s в одноразовом kind-кластере (bin/e2e-k8s-kind.sh)
k8s_e2e_kind:
	bin/e2e-k8s-kind.sh
