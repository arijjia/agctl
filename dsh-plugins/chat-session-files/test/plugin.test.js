import test from "node:test";
import assert from "node:assert/strict";
import { access, mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { apply } from "../lib/index.js";

test("new Session gets a hidden per-session Workspace and persistent cwd", async () => {
	const home = await mkdtemp(path.join(os.tmpdir(), "agctl-chat-files-"));
	const root = path.join(home, ".sessions");
	const oldRoot = process.env.AGCTL_CHAT_SESSION_ROOT;
	process.env.AGCTL_CHAT_SESSION_ROOT = root;
	try {
		const created = [];
		const createdWorkspaces = [];
		const attached = [];
		const existing = new Map();
		const workspaceRoot = path.dirname(root);
		const workspaces = new Map([
			["ws-agctl", { id: "ws-agctl", path: workspaceRoot }],
			["ws-project", { id: "ws-project", path: path.join(workspaceRoot, "Project") }],
		]);
		const registry = {
			get: (id) => workspaces.get(id),
			create: async (cwd, title) => {
				const workspace = { id: `hidden-${path.basename(cwd)}`, path: cwd, title };
				createdWorkspaces.push(workspace);
				workspaces.set(workspace.id, workspace);
				return workspace;
			},
		};
		const originalCreate = async (request) => {
			created.push(request);
			const id = request.sessionId;
			const workspace = workspaces.get(request.workspaceId);
			if (workspace) attached.push({ id, workspaceId: workspace.id });
			existing.set(id, { meta: { id, cwd: workspace?.path || request.cwd } });
			return { sessionId: id };
		};
		const controller = { create: originalCreate };
		const disposers = [];
		const ctx = {
			sessionController: controller,
			workspaceRegistry: registry,
			logger: { info: () => {} },
			effect: (register) => disposers.push(register()),
		};
		apply(ctx);

		const createdResult = await controller.create({ workspaceId: "ws-agctl", agentPreset: "default" });
		const id = createdResult.sessionId;
		const dir = path.join(root, id);
		await access(path.join(dir, ".agctl-session-workdir"));
		assert.equal(created.length, 1);
		assert.equal(created[0].cwd, undefined);
		assert.equal(created[0].agentPreset, "default");
		assert.equal(createdWorkspaces.length, 1);
		assert.equal(createdWorkspaces[0].path, dir);
		assert.equal(createdWorkspaces[0].title, "Chat");
		assert.equal(created[0].workspaceId, createdWorkspaces[0].id);
		assert.deepEqual(attached, [{ id, workspaceId: createdWorkspaces[0].id }]);

		// A caller-owned Session id remains under the DSH native idempotent contract.
		await controller.create({ sessionId: "session-caller-owned", workspaceId: "ws-agctl" });
		assert.equal(created.length, 2);
		assert.equal(createdWorkspaces.length, 1);
		assert.equal(created[1].sessionId, "session-caller-owned");
		assert.equal(created[1].workspaceId, "ws-agctl");

		// v3 project Workspace stays the real cwd; no hidden .sessions dir is made.
		const projectResult = await controller.create({ workspaceId: "ws-project", agentPreset: "default" });
		assert.equal(projectResult.sessionId, undefined); // DSH generates the id
		assert.equal(created.length, 3);
		assert.equal(createdWorkspaces.length, 1);
		assert.equal(created[2].workspaceId, "ws-project");
		assert.equal(created[2].cwd, undefined);

		for (const dispose of disposers) dispose?.();
		assert.equal(controller.create, originalCreate);
	} finally {
		if (oldRoot === undefined) delete process.env.AGCTL_CHAT_SESSION_ROOT;
		else process.env.AGCTL_CHAT_SESSION_ROOT = oldRoot;
		await rm(home, { recursive: true, force: true });
	}
});
