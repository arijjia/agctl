#!/usr/bin/env node
// L4-релей 0.0.0.0:RELAY_PORT -> 127.0.0.1:DSH_PORT. dsh web слушает только
// loopback (флаг --host 0.0.0.0 запрещён апстримом осознанно), а publish
// докера идёт на eth0 контейнера. Сырой TCP — HTTP+WS проходят как есть,
// Host/cookie не трогаем. Паттерн — kb/dsh/mgmt/docker/relay.mjs.
import net from 'node:net';

const listenPort = Number(process.env.RELAY_PORT ?? '3080');
const targetPort = Number(process.env.DSH_PORT ?? '3099');

const server = net.createServer((incoming) => {
  const outgoing = net.connect(targetPort, '127.0.0.1');
  const kill = () => {
    incoming.destroy();
    outgoing.destroy();
  };
  incoming.on('error', kill);
  outgoing.on('error', kill);
  incoming.pipe(outgoing).on('error', kill);
  outgoing.pipe(incoming).on('error', kill);
});
server.on('error', (e) => {
  process.stderr.write(`[agctl-relay] FATAL: ${e.message}\n`);
  process.exit(1);
});
server.listen(listenPort, '0.0.0.0', () => {
  process.stdout.write(`[agctl-relay] 0.0.0.0:${listenPort} -> 127.0.0.1:${targetPort}\n`);
});
