import { createClient } from "@connectrpc/connect";

import { AuthService } from "../gen/controlplane/v1/auth_pb";
import { ProjectService } from "../gen/controlplane/v1/projects_pb";
import { WorkspaceService } from "../gen/controlplane/v1/workspaces_pb";
import { transport } from "./connect";

// Generated, typed ConnectRPC clients shared across the app.
export const authClient = createClient(AuthService, transport);
export const projectClient = createClient(ProjectService, transport);
export const workspaceClient = createClient(WorkspaceService, transport);
