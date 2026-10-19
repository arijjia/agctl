import { randomUUID } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { sessionDirectory } from "./path.js";

const SCOPE_MARKER = ".agctl-session-workdir";

export const name = "agctl-chat-session-files";
export const inject = ["sessionController", "workspaceRegistry"];

/**
 * Host plugin for agctl Chat containers. Intercept new Session creation and
 * give the Session its own hidden cwd in the persistent /workspace volume.
 * Chat Sessions remain a flat/ungrouped history rather than appearing as
 * one visible Workspace folder per conversation.
 */
export function apply(ctx) {
	const rootValue = process.env.AGCTL_CHAT_SESSION_ROOT;
	if (!rootValue) return;

	const root = path.resolve(rootValue);
	const controller = ctx.sessionController;
	const registry = ctx.workspaceRegistry;
	const originalCreate = controller.create;
	const callOriginal = (request) => originalCreate.call(controller, request);

	const createWithSessionDirectory = async (request = {}) => {
		if (request.cwd !== undefined) return callOriginal(request);

		const sessionId = request.sessionId ?? `session-${randomUUID()}`;
		// Idempotent adoption must retain the Session's already-persisted cwd.
		if (request.sessionId !== undefined && typeof controller.inspect === "function") {
			try {
				const existing = await controller.inspect(sessionId);
				if (existing) return callOriginal(request);
			} catch {
				// Not found or not inspectable: create with the caller's id and a new cwd.
			}
		}

		const workspaceId = request.workspaceId;
		if (workspaceId !== undefined && !registry.get(workspaceId)) return callOriginal(request);

		const cwd = sessionDirectory(root, sessionId);
		await mkdir(cwd, { recursive: true, mode: 0o700 });
		await writeFile(path.join(cwd, SCOPE_MARKER), `session=${sessionId}\n`, { mode: 0o600, flag: "wx" }).catch((error) => {
			if (error?.code !== "EEXIST") throw error;
		});

		const { workspaceId: _workspaceId, ...rest } = request;
		return callOriginal({ ...rest, sessionId, cwd });
	};

	controller.create = createWithSessionDirectory;
	ctx.effect(() => () => {
		if (controller.create === createWithSessionDirectory) controller.create = originalCreate;
	});
	ctx.logger.info("agctl-chat-session-files: per-session cwd creation enabled");
}
