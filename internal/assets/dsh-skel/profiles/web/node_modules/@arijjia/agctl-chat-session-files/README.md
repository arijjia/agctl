# @arijjia/agctl-chat-session-files

Host-side DSH plugin for the default agctl Chat mode. It wraps the Host
`sessionController.create` call and gives each new DSH Session a hidden,
persistent working directory at `/workspace/.sessions/<sessionId>`. A matching
internal DSH Workspace is created at that path so the Session remains attached
and the composer works. The chat-brand client hides Workspace rows and presents
a flat session list; directory paths are not shown to Chat users.

The per-session folders are **not a security boundary** between sessions of
the same user: that user's shell can navigate to siblings.

The plugin activates only when `AGCTL_CHAT_SESSION_ROOT` is set by agctl on the
personal Chat container. Server dev-workspaces do not receive the variable and
keep their repository root as the shared project working directory.

These directories are session-associated storage, **not a security boundary
between sessions of the same user**: the user owns the whole container and may
navigate to sibling paths. Containers remain isolated between users. The
`/workspace` volume is persistent, so session files survive chat-container
sleep/recreation.
