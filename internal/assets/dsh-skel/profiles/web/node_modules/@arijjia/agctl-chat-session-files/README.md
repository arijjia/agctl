# @arijjia/agctl-chat-session-files

Host-side DSH plugin for the personal agctl Chat runtime. It wraps the Host
`sessionController.create` call and keeps the DSH Workspace selected by the
client when it points to a real project directory such as
`/workspace/<project>`.

For a new Session created in the default `/workspace` (or in a legacy hidden
session Workspace), the plugin creates a persistent per-session directory at
`/workspace/.sessions/<sessionId>` and an internal Workspace for that path.
This keeps the composer attached without exposing DSH's internal session
folders as project folders.

The chat-brand client registers project directories as real DSH Workspaces and
starts folder sessions with that Workspace ID. Those sessions must keep their
project cwd; they must not be redirected to `.sessions/<id>`.

Session and project directories are **not security boundaries** inside one
user's container: that user's shell can navigate to sibling paths. Containers
and volumes are isolated between users. The `/workspace` volume persists over
container sleep/recreation.

The plugin activates only when `AGCTL_CHAT_SESSION_ROOT` is set by agctl on the
personal runtime. It does not create or manage the retired legacy dev-workspace
seats.