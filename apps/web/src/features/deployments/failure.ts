// Plain-English classification of a failed deployment. The agent reports failures with stable
// message prefixes (see internal/agentcore/deploy.go) — "clone failed: …", "build failed: …",
// "could not start container: …", "health check failed: …", "Caddy routing failed: …", and so
// on — so we match on those and return a readable headline, a one-line explanation, and a fix
// that always preserves the recovery path. An unrecognized message still gets a usable summary
// that points at the logs. `stream` is the log tab that explains the failure, so the summary can
// link straight to it.

export type FailureStream = "build" | "runtime";

export interface FailureInfo {
  headline: string; // plain-English what happened
  explanation: string; // one sentence of why
  fix: string; // what to do next (keeps a recovery path)
  stream: FailureStream; // which log tab explains this failure
}

// Appended to every fix: a deploy that fails never disturbs the release that's already serving
// (supersede happens only after the new container is healthy) — say so, per principles.md.
const KEEP = " Any previous running release keeps serving.";

interface Rule {
  test: RegExp;
  headline: string;
  explanation: string;
  fix: string;
  stream: FailureStream;
}

// Order matters: the most specific prefixes first, the broad Caddy catch last before the
// fallback. All tests run against the deployment's latest reported message.
const RULES: Rule[] = [
  {
    test: /^Docker is not available/i,
    headline: "Docker isn't available on the server",
    explanation: "The agent couldn't reach Docker on the connected server, so it couldn't run your app.",
    fix: "Make sure Docker is installed and running on the server (the one-line installer sets this up), then deploy again.",
    stream: "build",
  },
  {
    test: /^Caddy routing is not configured/i,
    headline: "The server's reverse proxy isn't set up",
    explanation: "The agent has no Caddy to route public traffic through.",
    fix: "Install Caddy on the server (or set the agent's Caddy path), then deploy again.",
    stream: "build",
  },
  {
    test: /^clone failed/i,
    headline: "Couldn't fetch the repository",
    explanation: "The agent couldn't clone the repo at the branch or commit you chose.",
    fix: "Check the repository is public and the branch exists, then deploy again.",
    stream: "build",
  },
  {
    test: /^could not determine which port/i,
    headline: "Plorigo couldn't tell which port to expose",
    explanation: "The built image exposes no port and none was set on the service.",
    fix: "Add an EXPOSE to your Dockerfile, or set the container port on the service, then deploy again.",
    stream: "build",
  },
  {
    test: /^build failed/i,
    headline: "The build failed",
    explanation: "The image didn't build from your repository.",
    fix: "Open the build logs for the exact error — often a missing Dockerfile at the repo root or a failing build step — fix it, then deploy again.",
    stream: "build",
  },
  {
    test: /^(image pull failed|could not create a build workspace)/i,
    headline: "Couldn't prepare the image",
    explanation: "The agent couldn't pull the image (or set up a build workspace) on the server.",
    fix: "Check the image reference is correct and publicly pullable, then deploy again.",
    stream: "build",
  },
  {
    test: /^could not start container/i,
    headline: "The container failed to start",
    explanation: "The image was ready, but the container exited instead of running — usually the app crashes on startup.",
    fix: "Check the runtime logs for the app's own error (a missing env var or a bad start command is common), fix it, then deploy again.",
    stream: "runtime",
  },
  {
    test: /^health check failed/i,
    headline: "The app didn't become healthy",
    explanation: "The container started but never accepted connections on its port within the health-check window.",
    fix: "Make sure the app listens on the container port you configured and binds to 0.0.0.0 (not just localhost), then deploy again.",
    stream: "runtime",
  },
  {
    // Any remaining Caddy/route error: "could not derive Caddy route", "could not inspect
    // running containers for Caddy routes", "Caddy routing failed".
    test: /caddy/i,
    headline: "Couldn't route traffic to the app",
    explanation: "The app built and started, but the agent couldn't update Caddy to route to it.",
    fix: "Check Caddy is installed and running on the server, then deploy again.",
    stream: "build",
  },
];

// classifyFailure maps a failed deployment's message to a readable summary. sourceKind only
// steers the fallback's log-tab guess (a git deploy fails mostly in the build phase; a
// pre-built image in start/route).
export function classifyFailure(message: string, sourceKind = ""): FailureInfo {
  const msg = (message ?? "").trim();
  for (const r of RULES) {
    if (r.test.test(msg)) {
      return { headline: r.headline, explanation: r.explanation, fix: r.fix + KEEP, stream: r.stream };
    }
  }
  return {
    headline: "The deployment didn't finish",
    explanation: msg || "The deployment failed before the app was running.",
    fix: "Open the logs below for details, fix the issue, then deploy again." + KEEP,
    stream: sourceKind === "git" ? "build" : "runtime",
  };
}
