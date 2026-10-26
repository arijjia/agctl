# @arijjia/agctl-chat-session-files

Host-side plugin for the personal agctl DSH runtime. At boot it enumerates
`/workspace/*` and registers each visible project directory in the native DSH
WorkspaceRegistry via `workspaceRegistry.create(path)`. The native Workspaces
browser then owns folder icons, expand/collapse, session rows and status UI.

New sessions are created by the native Workspace client:

- in a project Workspace, DSH sets cwd to `/workspace/<project>`;
- without a selected project, DSH uses its default Workspace rooted at
  `/workspace`.

The plugin does **not** wrap `sessionController.create`, create hidden
`.sessions/<id>` directories, or render a custom sidebar. Dot-directories such
as legacy `.sessions/` are skipped during project registration.

Project directories are not security boundaries within one user's container:
the user's shell can navigate to sibling projects. Isolation is between users;
their containers and persistent bind mounts are separate. DSH_HOME (`/home/dsh`)
and `/workspace` are sibling mounts under the user's persistent host volume.