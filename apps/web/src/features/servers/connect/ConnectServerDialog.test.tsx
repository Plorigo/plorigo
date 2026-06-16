import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { renderWithClient } from "@/test/utils";

// Mock the generated RPC clients. vi.hoisted runs before the module imports below, so the
// factory can safely reference these fns.
const m = vi.hoisted(() => ({
  createServer: vi.fn(),
  createRegistrationToken: vi.fn(),
  startSetup: vi.fn(),
  getSetupRun: vi.fn(),
  listSetupEvents: vi.fn(),
}));

vi.mock("@/lib/clients", () => ({
  serverClient: { createServer: m.createServer },
  agentClient: { createRegistrationToken: m.createRegistrationToken },
  setupClient: {
    startSetup: m.startSetup,
    getSetupRun: m.getSetupRun,
    listSetupEvents: m.listSetupEvents,
  },
}));

import { ConnectServerDialog } from "./ConnectServerDialog";

function run(status: string, failureReason = "") {
  return {
    id: "run-1",
    serverId: "srv-1",
    status,
    failureReason,
    createdAt: "",
    updatedAt: "",
    finishedAt: "",
  };
}

function renderDialog() {
  const onManagedRun = vi.fn();
  renderWithClient(
    <ConnectServerDialog workspaceId="ws-1" open onOpenChange={vi.fn()} onManagedRun={onManagedRun} />,
  );
  return { onManagedRun };
}

beforeEach(() => {
  m.createServer.mockResolvedValue({ server: { id: "srv-1" } });
  m.createRegistrationToken.mockResolvedValue({
    installCommand: "curl -fsSL https://plorigo.test/install.sh | sudo sh -s -- --token plrt_secret",
    expiresAt: new Date("2026-01-01T00:00:00Z").toISOString(),
  });
  m.startSetup.mockResolvedValue({ run: run("running") });
  m.getSetupRun.mockResolvedValue({ run: run("succeeded") });
  m.listSetupEvents.mockResolvedValue({ events: [] });
});

async function gotoManaged(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("tab", { name: /plorigo sets it up/i }));
}

async function fillManaged(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText("Server name"), "prod-1");
  await user.type(screen.getByLabelText("Host or IP"), "203.0.113.10");
  await user.type(screen.getByLabelText("Password"), "hunter2");
}

describe("ConnectServerDialog — manual path", () => {
  it("creates a server and shows the one-time install command", async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText("Server name"), "prod-1");
    await user.click(screen.getByRole("button", { name: /generate install command/i }));

    expect(await screen.findByText(/--token plrt_secret/)).toBeInTheDocument();
    expect(m.createServer).toHaveBeenCalledWith({ workspaceId: "ws-1", name: "prod-1" });
    expect(m.createRegistrationToken).toHaveBeenCalledWith({ serverId: "srv-1" });
  });
});

describe("ConnectServerDialog — managed path", () => {
  it("starts a setup run and shows success when the run succeeds", async () => {
    const user = userEvent.setup();
    const { onManagedRun } = renderDialog();

    await gotoManaged(user);
    await fillManaged(user);
    await user.click(screen.getByRole("button", { name: /prepare server/i }));

    expect(await screen.findByText(/prod-1 is ready/i)).toBeInTheDocument();
    expect(m.startSetup).toHaveBeenCalledTimes(1);
    expect(m.startSetup.mock.calls[0][0]).toMatchObject({
      serverId: "srv-1",
      host: "203.0.113.10",
      port: 22,
      username: "root",
      password: "hunter2",
      privateKey: "",
    });
    expect(onManagedRun).toHaveBeenCalledWith("srv-1", "run-1");
    // The connecting step is rendered in the timeline.
    expect(screen.getByText("Connecting")).toBeInTheDocument();
  });

  it("shows a plain-English failure with the reason and recovery actions", async () => {
    m.getSetupRun.mockResolvedValue({
      run: run("failed", "unsupported operating system: CentOS Linux 7. Plorigo supports Ubuntu 22.04 and 24.04 LTS."),
    });
    const user = userEvent.setup();
    renderDialog();

    await gotoManaged(user);
    await fillManaged(user);
    await user.click(screen.getByRole("button", { name: /prepare server/i }));

    expect(await screen.findByText(/Setup failed/)).toBeInTheDocument();
    expect(screen.getByText(/unsupported operating system: CentOS/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry setup/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /use a command instead/i })).toBeInTheDocument();
  });

  it("returns to the form on retry and reuses the same server record", async () => {
    m.getSetupRun.mockResolvedValue({ run: run("failed", "the agent did not connect in time.") });
    const user = userEvent.setup();
    renderDialog();

    await gotoManaged(user);
    await fillManaged(user);
    await user.click(screen.getByRole("button", { name: /prepare server/i }));

    await user.click(await screen.findByRole("button", { name: /retry setup/i }));

    // Back on the form: re-enter the secret (it was cleared) and resubmit.
    expect(await screen.findByRole("button", { name: /prepare server/i })).toBeInTheDocument();
    await user.type(screen.getByLabelText("Password"), "hunter2");
    await user.click(screen.getByRole("button", { name: /prepare server/i }));

    await waitFor(() => expect(m.startSetup).toHaveBeenCalledTimes(2));
    // The server was created once and reused for the retry — no duplicate server.
    expect(m.createServer).toHaveBeenCalledTimes(1);
  });
});
