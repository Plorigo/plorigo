// Curated, hardcoded deploy templates for the "Add a service" gallery. This is intentionally
// a static list (no templates backend). A template is one of three kinds:
//   - "image": a known prebuilt public image, deployable today.
//   - "repo": a public Git repo, built from its Dockerfile.
//   - "managed": a control-plane-provisioned managed service (e.g. a database) — the image and
//     port are fixed server-side; the user only adjusts the exposed options.
// Every template declares the `options` the "Configure & deploy" dialog asks before creating,
// so adding a new managed service (MongoDB, Redis, …) is a data-only change here plus its
// backend catalogue entry. Keep entries to public images/repos or known managed templates.

export type TemplateCategory = "Starter" | "Web" | "API" | "Database";

export type TemplateKind = "image" | "repo" | "managed";

export type TemplateOptionType = "text" | "number" | "password" | "select";

export interface TemplateOptionChoice {
  label: string;
  value: string;
}

// A single field shown in the template's configure dialog. The values the user fills become the
// create call's arguments (keyed by `key`). `editable: false` renders the value read-only (e.g.
// a managed database's fixed port). `optional` fields may be left blank.
export interface TemplateOption {
  key: string;
  label: string;
  type: TemplateOptionType;
  default?: string;
  placeholder?: string;
  help?: string;
  editable?: boolean; // default true; false = display only
  optional?: boolean; // default false; true = may be blank
  generated?: boolean; // seed with a freshly generated secret the user can see/copy/edit
  choices?: TemplateOptionChoice[]; // for type "select"
}

export interface DeployTemplate {
  id: string;
  name: string;
  description: string;
  category: TemplateCategory;
  kind: TemplateKind;
  // Exactly one source applies to the kind:
  imageRef?: string; // kind "image"
  repoUrl?: string; // kind "repo"
  defaultBranch?: string; // kind "repo"
  managedTemplateId?: string; // kind "managed" — the backend catalogue id, e.g. "postgres"
  containerPort: number; // the port the service listens on inside the container
  // The fields the configure dialog asks before deploying.
  options: TemplateOption[];
}

const VISIBILITY_OPTION: TemplateOption = {
  key: "visibility",
  label: "Visibility",
  type: "select",
  default: "public",
  choices: [
    { label: "Public", value: "public" },
    { label: "Private", value: "private" },
  ],
  help: "Public services get a routable URL; private ones are reachable only by siblings.",
};

// imageOptions / repoOptions build the dialog fields for the plain template kinds. The service
// name defaults to the template id; the port defaults to the template's known port.
function imageOptions(id: string, port: number): TemplateOption[] {
  return [
    { key: "name", label: "Service name", type: "text", default: id, placeholder: id },
    { key: "port", label: "Container port", type: "number", default: String(port) },
    VISIBILITY_OPTION,
  ];
}

function repoOptions(id: string, branch: string, port: number): TemplateOption[] {
  return [
    { key: "name", label: "Service name", type: "text", default: id, placeholder: id },
    { key: "branch", label: "Branch", type: "text", default: branch, optional: true, placeholder: "default" },
    {
      key: "port",
      label: "Container port",
      type: "number",
      default: String(port),
      help: "0 = auto-detect from the Dockerfile EXPOSE.",
    },
    VISIBILITY_OPTION,
  ];
}

export const deployTemplates: DeployTemplate[] = [
  {
    id: "whoami",
    name: "whoami",
    description: "Tiny HTTP service that echoes the request. A perfect first deploy.",
    category: "Starter",
    kind: "image",
    imageRef: "traefik/whoami:latest",
    containerPort: 80,
    options: imageOptions("whoami", 80),
  },
  {
    id: "nginx-hello",
    name: "NGINX Hello",
    description: "A static NGINX welcome page — verify routing and health checks.",
    category: "Web",
    kind: "image",
    imageRef: "nginxdemos/hello:latest",
    containerPort: 80,
    options: imageOptions("nginx-hello", 80),
  },
  {
    id: "httpbin",
    name: "httpbin",
    description: "An HTTP request & response testing service with handy endpoints.",
    category: "API",
    kind: "image",
    imageRef: "kennethreitz/httpbin:latest",
    containerPort: 80,
    options: imageOptions("httpbin", 80),
  },
  {
    id: "welcome-to-docker",
    name: "Welcome to Docker",
    description: "Docker's sample web app, built from the repository's Dockerfile.",
    category: "Web",
    kind: "repo",
    repoUrl: "https://github.com/docker/welcome-to-docker",
    defaultBranch: "main",
    containerPort: 8080,
    options: repoOptions("welcome-to-docker", "main", 8080),
  },
  {
    id: "postgres",
    name: "PostgreSQL",
    description: "A managed Postgres database with generated credentials, private to its environment.",
    category: "Database",
    kind: "managed",
    managedTemplateId: "postgres",
    containerPort: 5432,
    options: [
      { key: "name", label: "Service name", type: "text", default: "postgres", placeholder: "postgres" },
      {
        key: "databaseName",
        label: "Database name",
        type: "text",
        default: "app",
        placeholder: "app",
        help: "The initial database created in the cluster.",
      },
      { key: "username", label: "Username", type: "text", default: "plorigo", placeholder: "plorigo" },
      {
        key: "password",
        label: "Password",
        type: "password",
        generated: true,
        help: "Generated for you — copy it, edit it, or regenerate. Stored as POSTGRES_PASSWORD.",
      },
      {
        key: "port",
        label: "Port",
        type: "number",
        default: "5432",
        editable: false,
        help: "Managed databases use their standard port.",
      },
    ],
  },
];
