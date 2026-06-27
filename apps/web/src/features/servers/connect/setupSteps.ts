import type { ServerSetupEvent } from "@/gen/controlplane/v1/serversetup_pb";

// The ordered bootstrap steps, with plain-English labels for the timeline. The keys match the
// `step` field the control plane emits on setup events (see internal/serversetup/bootstrap.go).
// "connect" is emitted by the service before the runner; the rest are the runner's steps.
export interface SetupStepMeta {
  key: string;
  label: string;
  detail: string;
}

export const SETUP_STEPS: SetupStepMeta[] = [
  { key: "connect", label: "Connecting", detail: "Opening an SSH session and pinning the host key" },
  { key: "detect_os", label: "Detecting OS", detail: "Confirming Ubuntu 22.04 / 24.04 LTS" },
  { key: "check_privilege", label: "Checking access", detail: "Verifying root or passwordless sudo" },
  { key: "preflight", label: "Pre-flight checks", detail: "apt lock, ports 80/443, Docker, firewall" },
  { key: "install_prereqs", label: "Installing Docker, Caddy & agent", detail: "Running the Plorigo installer" },
  { key: "provision_user", label: "Provisioning management user", detail: "Creating the plorigo user, key & scoped sudo" },
  { key: "await_agent", label: "Waiting for heartbeat", detail: "The agent dials home and comes online" },
];

export type StepState = "pending" | "active" | "done" | "failed";

// deriveStepStates maps the latest status event per step to a timeline state. A succeeded run
// marks every step done; a failed run leaves the failing step failed and steps after it pending.
export function deriveStepStates(
  events: ServerSetupEvent[],
  runStatus: string,
): Record<string, StepState> {
  const latest: Record<string, string> = {};
  for (const e of events) {
    if (e.kind === "status" && e.status) latest[e.step] = e.status;
  }
  const states: Record<string, StepState> = {};
  for (const step of SETUP_STEPS) {
    const s = latest[step.key];
    if (runStatus === "succeeded") {
      states[step.key] = "done";
      continue;
    }
    switch (s) {
      case "ok":
      case "skipped":
        states[step.key] = "done";
        break;
      case "started":
        states[step.key] = "active";
        break;
      case "failed":
        states[step.key] = "failed";
        break;
      default:
        states[step.key] = "pending";
    }
  }
  return states;
}
