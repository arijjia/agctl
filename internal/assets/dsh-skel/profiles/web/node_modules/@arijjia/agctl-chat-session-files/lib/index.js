import { randomUUID } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { sessionDirectory } from "./path.js";

const SCOPE_MARKER = ".agctl-session-workdir";

export const name = "agctl-chat-session-files";
export const inject = ["sessionController", "workspaceRegistry"];

/**
 * Host plugin for agctl Chat containers. Sessions in the default Workspace get
 * a hidden per-session directory. Sessions explicitly started in a project
 * Workspace keep that real project cwd, so folder=project is physical on disk.
 * The chat-brand client controls which project Workspace starts a new Session.
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

		// Caller-owned ids may be idempotent re-adoptions; keep the DSH contract
		// untouched rather than creating a second hidden Workspace.
		if (request.sessionId !== undefined) return callOriginal(request);
		const sessionId = `session-${randomUUID()}`;

		const requestedWorkspaceId = request.workspaceId;
		const requestedWorkspace = requestedWorkspaceId !== undefined ? registry.get(requestedWorkspaceId) : undefined;
		if (requestedWorkspaceId !== undefined && !requestedWorkspace) return callOriginal(request);
		// v3: проект-папка является настоящим DSH Workspace. Сессию в нём
		// оставляем в каталоге проекта; скрытый .sessions/<id> создаём только
		// для обычного чата/default /workspace.
		const workspaceRoot = path.dirname(root);
		const requestedPath = requestedWorkspace && path.resolve(requestedWorkspace.path);
		const sessionRoot = path.resolve(root);
		const isPerSessionPath = requestedPath === sessionRoot || requestedPath.startsWith(sessionRoot + path.sep);
		if (requestedWorkspace && requestedPath !== workspaceRoot && !isPerSessionPath) {
			return callOriginal(request);
		}

		const cwd = sessionDirectory(root, sessionId);
		await mkdir(cwd, { recursive: true, mode: 0o700 });
		await writeFile(path.join(cwd, SCOPE_MARKER), `session=${sessionId}\n`, { mode: 0o600, flag: "wx" }).catch((error) => {
			if (error?.code !== "EEXIST") throw error;
		});
		// DSH rc2 rejects workspaceId+cwd together, but the web composer requires
		// every new Session to belong to a Workspace. Create a hidden per-session
		// Workspace at the same directory, then pass only that WorkspaceId.
		const workspace = await registry.create(cwd, "Chat");
		const { workspaceId: _workspaceId, cwd: _cwd, ...rest } = request;
		return callOriginal({ ...rest, sessionId, workspaceId: workspace.id });
	};

	controller.create = createWithSessionDirectory;
	ctx.effect(() => () => {
		if (controller.create === createWithSessionDirectory) controller.create = originalCreate;
	});
	ctx.logger.info("agctl-chat-session-files: per-session cwd creation enabled");
}
