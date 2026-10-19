import path from "node:path";

const SAFE_SESSION_ID = /^[A-Za-z0-9_-]{1,128}$/;

/** Resolve a DSH Session id to one child directory beneath the per-user root. */
export function sessionDirectory(root, sessionId) {
	if (typeof root !== "string" || !path.isAbsolute(root)) {
		throw new TypeError("agctl chat session root must be absolute");
	}
	if (typeof sessionId !== "string" || !SAFE_SESSION_ID.test(sessionId)) {
		throw new TypeError("agctl chat session id is not a safe path segment");
	}
	const rootPath = path.resolve(root);
	const target = path.resolve(rootPath, sessionId);
	if (!target.startsWith(rootPath + path.sep)) {
		throw new TypeError("agctl chat session path escaped its root");
	}
	return target;
}
