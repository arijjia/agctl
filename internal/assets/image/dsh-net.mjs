#!/usr/bin/env node
// Сетевая утилита образа agctl-dsh. Релея в образе больше нет: dsh web
// слушает конкретный eth0-адрес контейнера (апстрим dsh запрещает только
// wildcard-бинд, --host 10.0.0.7 легален), наружу его отдаёт публикация
// порта Docker/k8s. Два режима:
//   dsh-net print   — напечатать адрес для бинда (использует entrypoint)
//   dsh-net health  — exit 0, если dsh отвечает (<500); иначе 1 (HEALTHCHECK)
import http from 'node:http';
import os from 'node:os';

const port = Number(process.env.DSH_PORT ?? '3080');

// eth0 — первый кандидат (docker bridge и pod-сеть), далее любой не-internal IPv4.
function bindIp() {
  const all = os.networkInterfaces();
  const candidates = [...(all.eth0 ?? [])];
  for (const [name, addrs] of Object.entries(all)) {
    if (name !== 'eth0') candidates.push(...(addrs ?? []));
  }
  return candidates.find((a) => a && !a.internal && a.family === 'IPv4')?.address;
}

const mode = process.argv[2] ?? 'print';
if (mode === 'print') {
  const ip = bindIp();
  if (ip === undefined) {
    process.stderr.write('[agctl-dsh-net] FATAL: не-internal IPv4 интерфейс не найден\n');
    process.exit(1);
  }
  process.stdout.write(ip);
} else if (mode === 'health') {
  const ip = bindIp();
  if (ip === undefined) process.exit(1);
  const req = http.get({ host: ip, port, path: '/' }, (res) => {
    res.resume();
    process.exit(res.statusCode < 500 ? 0 : 1);
  });
  req.on('error', () => process.exit(1));
  req.setTimeout(5000, () => {
    req.destroy();
    process.exit(1);
  });
} else {
  process.stderr.write(`[agctl-dsh-net] unknown mode: ${mode}\n`);
  process.exit(2);
}
