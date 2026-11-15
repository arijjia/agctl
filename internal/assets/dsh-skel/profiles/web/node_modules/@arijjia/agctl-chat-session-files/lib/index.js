import path from "node:path";
import { readdir, stat } from "node:fs/promises";

export const name = "agctl-chat-session-files";
export const inject = ["workspaceRegistry"];

/**
 * Host plugin for the personal agctl runtime. At boot it registers every
 * project directory under /workspace as a real DSH Workspace, so the native
 * Workspaces sidebar shows project folders with native icons and statuses.
 *
 * Sessions always belong to a real Workspace: project sessions keep their
 * project cwd (/workspace/<name>); unassigned sessions live in the default
 * Workspace rooted at /workspace. No hidden per-session directories are
 * created — the native sidebar stays clean.
 *
 * Local mode (agctl dev, AGCTL_LOCAL_MODE=1): /workspace is the developer's
 * own folder, and it can be switched between runs while dsh-home stays the
 * same. Before registering, stale registrations under /workspace whose
 * directory no longer exists are removed (registry.delete keeps the
 * directory and every session log), so the sidebar shows only folders of the
 * current mount. Server mode never prunes.
 *
 * Workspace directories are NOT security boundaries inside one user's
 * container: that user's shell can navigate anywhere in /workspace.
 * Containers and volumes remain isolated between users.
 */
export function apply(ctx) {
	const registry = ctx.workspaceRegistry;
	if (!registry || typeof registry.resolveByPath !== "function" || typeof registry.create !== "function") return;

	// /workspace inside the container; AGCTL_WORKSPACE_ROOT exists only for tests.
	const workspaceRoot = process.env.AGCTL_WORKSPACE_ROOT || "/workspace";
	const localMode = process.env.AGCTL_LOCAL_MODE === "1";
	const pruneStale = async () => {
		if (!localMode || typeof registry.list !== "function" || typeof registry.delete !== "function") return;
		for (const workspace of registry.list()) {
			const dir = workspace && workspace.path;
			if (typeof dir !== "string" || !dir.startsWith(workspaceRoot + "/")) continue;
			try {
				if ((await stat(dir)).isDirectory()) continue;
			} catch {
				// нет папки — снимаем регистрацию ниже
			}
			try {
				await registry.delete(workspace.id);
				ctx.logger.info(`agctl-chat-session-files: stale workspace removed for ${dir}`);
			} catch (error) {
				ctx.logger.info(`agctl-chat-session-files: stale workspace kept for ${dir}: ${error?.message ?? error}`);
			}
		}
	};
	const registerProjects = async () => {
		await pruneStale();
		let entries;
		try {
			entries = await readdir(workspaceRoot, { withFileTypes: true });
		} catch (error) {
			ctx.logger.info(`agctl-chat-session-files: cannot list ${workspaceRoot}: ${error?.message ?? error}`);
			return;
		}
		for (const entry of entries) {
			if (!entry.isDirectory() || entry.name.startsWith(".")) continue;
			const dir = path.join(workspaceRoot, entry.name);
			try {
				const known = await registry.resolveByPath(dir);
				if (known === undefined) {
					await registry.create(dir);
					ctx.logger.info(`agctl-chat-session-files: workspace registered for ${dir}`);
				}
			} catch (error) {
				ctx.logger.info(`agctl-chat-session-files: workspace registration skipped for ${dir}: ${error?.message ?? error}`);
			}
		}
	};
	void registerProjects();
	ctx.logger.info("agctl-chat-session-files: native project workspace registration enabled");
}
