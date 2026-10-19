import test from "node:test";
import assert from "node:assert/strict";
import { access, mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { apply } from "../lib/index.js";

test("new Session gets one persistent cwd without mutating an existing Workspace", async () => {
	const home = await mkdtemp(path.join(os.tmpdir(), "agctl-chat-files-"));
	const root = path.join(home, ".sessions");
	const oldRoot = process.env.AGCTL_CHAT_SESSION_ROOT;
	process.env.AGCTL_CHAT_SESSION_ROOT = root;
	try {
		const created = [];
		const attached = [];
		const existing = new Map();
		const workspace = { attachSession: async (id) => attached.push(id) };
		const originalCreate = async (request) => {
			created.push(request);
			const id = request.sessionId;
			existing.set(id, { meta: { id, cwd: request.cwd } });
			return { sessionId: id };
		};
		const controller = {
			inspect: async (id) => existing.get(id),
			create: originalCreate,
		};
		const disposers = [];
		const ctx = {
			sessionController: controller,
			workspaceRegistry: { get: (id) => (id === "ws-agctl" ? workspace : undefined) },
			logger: { info: () => {} },
			effect: (register) => disposers.push(register()),
		};
		apply(ctx);

		const createdResult = await controller.create({ workspaceId: "ws-agctl", agentPreset: "default" });
		const id = createdResult.sessionId;
		const dir = path.join(root, id);
		await access(path.join(dir, ".agctl-session-workdir"));
		assert.equal(created[0].cwd, dir);
		assert.equal(created[0].workspaceId, undefined);
		assert.equal(created[0].agentPreset, "default");
		assert.deepEqual(attached, []);

		// Idempotent re-adoption preserves the existing cwd and does not allocate a new folder.
		await controller.create({ sessionId: id, workspaceId: "ws-agctl" });
		assert.equal(created.length, 2);
		assert.equal(created[1].sessionId, id);
		assert.equal(created[1].cwd, undefined);
		assert.deepEqual(attached, []);

		for (const dispose of disposers) dispose?.();
		assert.equal(controller.create, originalCreate);
	} finally {
		if (oldRoot === undefined) delete process.env.AGCTL_CHAT_SESSION_ROOT;
		else process.env.AGCTL_CHAT_SESSION_ROOT = oldRoot;
		await rm(home, { recursive: true, force: true });
	}
});
