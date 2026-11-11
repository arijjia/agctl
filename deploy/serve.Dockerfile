# agctl serve — образ шлюза для Kubernetes (deploy/k8s). Бинарь статический
# (CGO_ENABLED=0, SQLite — modernc, UI вшит go:embed), образ без shell.
# Контекст сборки — корень репо после `make go_linux_amd64` (dist/agctl-linux-<arch>).
# uid 1000 совпадает с uid dsh-подов: общий RWX-том (shared claim) читается
# и пишется обеими сторонами без chown.
ARG TARGETARCH=amd64
FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETARCH
COPY dist/agctl-linux-${TARGETARCH} /usr/local/bin/agctl
USER 1000:1000
ENV AGCTL_HOME=/var/lib/agctl
EXPOSE 8929
ENTRYPOINT ["/usr/local/bin/agctl"]
CMD ["serve", "--addr", "0.0.0.0:8929"]
