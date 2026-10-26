import path from "node:path";
import { readdir } from "node:fs/promises";

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
 * Workspace directories are NOT security boundaries inside one user's
 * container: that user's shell can navigate anywhere in /workspace.
 * Containers and volumes remain isolated between users.
 */
export function apply(ctx) {
	const registry = ctx.workspaceRegistry;
	if (!registry || typeof registry.resolveByPath !== "function" || typeof registry.create !== "function") return;

	// /workspace inside the container; AGCTL_WORKSPACE_ROOT exists only for tests.
	const workspaceRoot = process.env.AGCTL_WORKSPACE_ROOT || "/workspace";
	const registerProjects = async () => {
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
