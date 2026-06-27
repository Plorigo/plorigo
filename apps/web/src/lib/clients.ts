import { createClient } from "@connectrpc/connect";

import { AgentService } from "../gen/controlplane/v1/agents_pb";
import { AuthService } from "../gen/controlplane/v1/auth_pb";
import { ConfigService } from "../gen/controlplane/v1/config_pb";
import { DeploymentService } from "../gen/controlplane/v1/deployments_pb";
import { DomainService } from "../gen/controlplane/v1/domains_pb";
import { EnvironmentService } from "../gen/controlplane/v1/environments_pb";
import { ProjectService } from "../gen/controlplane/v1/projects_pb";
import { ServerService } from "../gen/controlplane/v1/servers_pb";
import { ServiceService } from "../gen/controlplane/v1/services_pb";
import { SourceService } from "../gen/controlplane/v1/sources_pb";
import { WorkspaceService } from "../gen/controlplane/v1/workspaces_pb";
import { transport } from "./connect";

// Generated, typed ConnectRPC clients shared across the app.
export const agentClient = createClient(AgentService, transport);
export const authClient = createClient(AuthService, transport);
export const configClient = createClient(ConfigService, transport);
export const deploymentClient = createClient(DeploymentService, transport);
export const domainClient = createClient(DomainService, transport);
export const environmentClient = createClient(EnvironmentService, transport);
export const projectClient = createClient(ProjectService, transport);
export const serverClient = createClient(ServerService, transport);
export const serviceClient = createClient(ServiceService, transport);
export const sourceClient = createClient(SourceService, transport);
export const workspaceClient = createClient(WorkspaceService, transport);
