import test from "node:test";
import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { apply } from "../lib/index.js";

async function workspaceRoot(t) {
	const parent = await mkdtemp(path.join(os.tmpdir(), "agctl-ws-root-"));
	const dir = path.join(parent, "workspace");
	t.after(() => rm(parent, { recursive: true, force: true }));
	await mkdir(path.join(dir, "1"), { recursive: true });
	await mkdir(path.join(dir, "2"), { recursive: true });
	await mkdir(path.join(dir, ".sessions"), { recursive: true });
	return dir;
}

// Registration is fired-and-forgotten: let the async readdir/create chain drain.
const settle = () => new Promise((resolve) => setTimeout(resolve, 50));

test("boot registers every project directory as a native DSH Workspace", async (t) => {
	const root = await workspaceRoot(t);
	const created = [];
	const known = new Map();
	const registry = {
		async resolveByPath(dir) {
			return known.get(dir);
		},
		async create(dir) {
			const workspace = { id: `ws-${dir}`, path: dir };
			created.push(workspace);
			known.set(dir, workspace);
			return workspace;
		},
	};
	const logs = [];
	const oldRoot = process.env.AGCTL_WORKSPACE_ROOT;
	process.env.AGCTL_WORKSPACE_ROOT = root;
	try {
		apply({ workspaceRegistry: registry, logger: { info: (line) => logs.push(line) } });
		await settle();
	} finally {
		if (oldRoot === undefined) delete process.env.AGCTL_WORKSPACE_ROOT;
		else process.env.AGCTL_WORKSPACE_ROOT = oldRoot;
	}

	assert.deepEqual(created.map((workspace) => workspace.path).sort(), [
		path.join(root, "1"),
		path.join(root, "2"),
	], "dot directories (.sessions) are skipped");
	assert.ok(logs.some((line) => line.includes("native project workspace registration enabled")));
});

test("boot skips already registered workspaces", async (t) => {
	const root = await workspaceRoot(t);
	const registered = path.join(root, "1");
	const created = [];
	const registry = {
		async resolveByPath(dir) {
			return dir === registered ? { id: "ws-1", path: dir } : undefined;
		},
		async create(dir) {
			created.push({ path: dir });
			return { id: `ws-${dir}`, path: dir };
		},
	};
	const oldRoot = process.env.AGCTL_WORKSPACE_ROOT;
	process.env.AGCTL_WORKSPACE_ROOT = root;
	try {
		apply({ workspaceRegistry: registry, logger: { info: () => {} } });
		await settle();
	} finally {
		if (oldRoot === undefined) delete process.env.AGCTL_WORKSPACE_ROOT;
		else process.env.AGCTL_WORKSPACE_ROOT = oldRoot;
	}

	assert.deepEqual(created.map((workspace) => workspace.path), [path.join(root, "2")]);
});

test("plugin no longer wraps sessionController and needs no session root env", async (t) => {
	const root = await workspaceRoot(t);
	const ctx = {
		workspaceRegistry: {
			async resolveByPath() {
				return undefined;
			},
			async create(dir) {
				return { id: "ws", path: dir };
			},
		},
		logger: { info: () => {} },
	};
	const controller = { create: () => {} };
	ctx.sessionController = controller;
	const oldRoot = process.env.AGCTL_WORKSPACE_ROOT;
	const oldSessionRoot = process.env.AGCTL_CHAT_SESSION_ROOT;
	process.env.AGCTL_WORKSPACE_ROOT = root;
	delete process.env.AGCTL_CHAT_SESSION_ROOT;
	try {
		apply(ctx);
		await settle();
	} finally {
		if (oldRoot === undefined) delete process.env.AGCTL_WORKSPACE_ROOT;
		else process.env.AGCTL_WORKSPACE_ROOT = oldRoot;
		if (oldSessionRoot !== undefined) process.env.AGCTL_CHAT_SESSION_ROOT = oldSessionRoot;
	}

	assert.equal(controller.create, ctx.sessionController.create, "sessionController must stay untouched");
});
