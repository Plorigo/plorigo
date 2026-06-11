// Curated, hardcoded deploy templates that prefill the simple "Add Project" wizard.
// This is intentionally a static list (no templates backend) — see the add-project
// plan. A template is either a public Git repo (built from its Dockerfile once the
// build pipeline lands) or a known prebuilt image (deployable today). The shape
// carries just enough to prefill the wizard: a name, a port, and optional env.
//
// This list is meant to be curated and extended over time. Keep entries to public
// images/repos with a known container port; do not add database services here (the
// platform does not provision databases yet).

export type TemplateCategory = "Starter" | "Web" | "API";

export interface DeployTemplate {
  id: string;
  name: string;
  description: string;
  category: TemplateCategory;
  // Exactly one source is set:
  imageRef?: string; // a known prebuilt image — deployable today
  repoUrl?: string; // OR a public repo, built from its Dockerfile
  defaultBranch?: string;
  containerPort: number; // the port the app listens on inside the container
  suggestedEnv?: Array<{ key: string; value?: string; note?: string }>;
}

export const deployTemplates: DeployTemplate[] = [
  {
    id: "whoami",
    name: "whoami",
    description: "Tiny HTTP service that echoes the request. A perfect first deploy.",
    category: "Starter",
    imageRef: "traefik/whoami:latest",
    containerPort: 80,
  },
  {
    id: "nginx-hello",
    name: "NGINX Hello",
    description: "A static NGINX welcome page — verify routing and health checks.",
    category: "Web",
    imageRef: "nginxdemos/hello:latest",
    containerPort: 80,
  },
  {
    id: "httpbin",
    name: "httpbin",
    description: "An HTTP request & response testing service with handy endpoints.",
    category: "API",
    imageRef: "kennethreitz/httpbin:latest",
    containerPort: 80,
  },
  {
    id: "welcome-to-docker",
    name: "Welcome to Docker",
    description: "Docker's sample web app, built from the repository's Dockerfile.",
    category: "Web",
    repoUrl: "https://github.com/docker/welcome-to-docker",
    defaultBranch: "main",
    containerPort: 8080,
  },
];

export function templateSourceKind(template: DeployTemplate): "image" | "repo" {
  return template.imageRef ? "image" : "repo";
}
