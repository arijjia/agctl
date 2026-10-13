#!/usr/bin/env python3
"""fake-llm — одноразовый OpenAI-совместимый стаб для e2e agctl.

Отвечает на POST /v1/chat/completions каноничным JSON с фиксированным
ответом (маркер FAKE-LLM-REPLY), на GET /v1/models — списком моделей.
Нужен только чтобы проверить живой провод provision → контейнер → модель
без реального токена. Секретов не содержит; наружу слушает loopback.

Запуск: scripts/fake-llm.py [--port 18765] [--reply "..."]
"""
import argparse
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ARGS = None  # type: argparse.Namespace


class Handler(BaseHTTPRequestHandler):
    server_version = "fake-llm/1"

    def _send(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):  # noqa: N802
        if self.path.rstrip("/").endswith("/models"):
            self._send(200, {
                "object": "list",
                "data": [{"id": m, "object": "model"} for m in ARGS.models],
            })
        else:
            self._send(404, {"error": {"message": "not found"}})

    def do_POST(self):  # noqa: N802
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n) if n else b"{}"
        try:
            req = json.loads(raw or b"{}")
        except Exception:
            req = {}
        model = req.get("model") or (ARGS.models[0] if ARGS.models else "stub-model")
        stream = bool(req.get("stream"))
        if stream:
            # SSE-поток: один чанк с полным ответом и terminator.
            chunk = {
                "id": "chatcmpl-fake", "object": "chat.completion.chunk",
                "created": int(time.time()), "model": model,
                "choices": [{"index": 0, "delta": {"role": "assistant",
                            "content": ARGS.reply}, "finish_reason": None}],
            }
            done = {"id": "chatcmpl-fake", "object": "chat.completion.chunk",
                    "created": int(time.time()), "model": model,
                    "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}]}
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            for part in (chunk, done):
                self.wfile.write(b"data: " + json.dumps(part).encode() + b"\n\n")
            self.wfile.write(b"data: [DONE]\n\n")
            return
        self._send(200, {
            "id": "chatcmpl-fake", "object": "chat.completion",
            "created": int(time.time()), "model": model,
            "choices": [{"index": 0, "message": {
                "role": "assistant", "content": ARGS.reply},
                "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 1, "completion_tokens": 1,
                      "total_tokens": 2},
        })

    def log_message(self, fmt, *args):  # шум в stderr с меткой
        sys.stderr.write("[fake-llm] " + fmt % args + "\n")


def main():
    global ARGS
    p = argparse.ArgumentParser()
    p.add_argument("--port", type=int, default=18765)
    p.add_argument("--reply", default="FAKE-LLM-REPLY: всё работает")
    p.add_argument("--models", default="stub-model",
                   help="запятая-раздёл перечисление id моделей")
    ARGS = p.parse_args()
    ARGS.models = [m.strip() for m in ARGS.models.split(",") if m.strip()]
    srv = ThreadingHTTPServer(("127.0.0.1", ARGS.port), Handler)
    sys.stderr.write(f"[fake-llm] listening on 127.0.0.1:{ARGS.port}\n")
    sys.stderr.flush()
    srv.serve_forever()


if __name__ == "__main__":
    main()
