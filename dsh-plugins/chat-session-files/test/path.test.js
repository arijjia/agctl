import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { sessionDirectory } from "../lib/path.js";

test("sessionDirectory creates a child path for a normal DSH id", async () => {
	const root = await mkdtemp(path.join(os.tmpdir(), "agctl-chat-session-"));
	try {
		assert.equal(sessionDirectory(root, "session-abc-123"), path.join(root, "session-abc-123"));
	} finally {
		await rm(root, { recursive: true, force: true });
	}
});

test("sessionDirectory rejects path traversal, separators and non-absolute roots", () => {
	const root = "/workspace/.sessions";
	for (const id of ["../other", "/etc/passwd", "session/a", "", ".."])
		assert.throws(() => sessionDirectory(root, id), TypeError);
	assert.throws(() => sessionDirectory("relative/sessions", "session-abc"), TypeError);
});
