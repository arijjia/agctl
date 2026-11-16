# agctl-server serve — образ шлюза для Kubernetes (deploy/k8s). Бинарь статический
# (CGO_ENABLED=0, SQLite — modernc, UI вшит go:embed), образ без shell.
# Контекст сборки — корень репо после `make img_serve_build` (dist/agctl-server-linux-<arch>).
# uid 1000 совпадает с uid dsh-подов: общий RWX-том (shared claim) читается
# и пишется обеими сторонами без chown.
ARG TARGETARCH=amd64
FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETARCH
COPY dist/agctl-server-linux-${TARGETARCH} /usr/local/bin/agctl-server
USER 1000:1000
ENV AGCTL_HOME=/var/lib/agctl
EXPOSE 8929
ENTRYPOINT ["/usr/local/bin/agctl-server"]
CMD ["serve", "--addr", "0.0.0.0:8929"]
