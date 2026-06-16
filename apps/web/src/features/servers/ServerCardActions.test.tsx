import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { renderWithClient } from "@/test/utils";

const m = vi.hoisted(() => ({
  getManagementKey: vi.fn(),
  rotateManagementKey: vi.fn(),
  revokeManagementKey: vi.fn(),
}));

vi.mock("@/lib/clients", () => ({
  setupClient: {
    getManagementKey: m.getManagementKey,
    rotateManagementKey: m.rotateManagementKey,
    revokeManagementKey: m.revokeManagementKey,
  },
}));

import { ServerCardActions } from "./ServerCardActions";

function key(revokedAt = "") {
  return {
    serverId: "srv-1",
    fingerprint: "SHA256:abc",
    publicKey: "ssh-ed25519 AAAA",
    rotationState: "active",
    lastUsedAt: "",
    rotatedAt: "",
    revokedAt,
    createdBy: "",
    createdAt: "",
    updatedAt: "",
  };
}

function renderActions() {
  renderWithClient(
    <ServerCardActions
      serverId="srv-1"
      serverName="prod-1"
      minting={false}
      onInstallCommand={vi.fn()}
      onDelete={vi.fn()}
    />,
  );
}

beforeEach(() => {
  m.getManagementKey.mockResolvedValue({ key: key() });
  m.rotateManagementKey.mockResolvedValue({ key: key() });
  m.revokeManagementKey.mockResolvedValue({});
});

describe("ServerCardActions — SSH credential affordances", () => {
  it("rotates the management key after confirmation when an active key exists", async () => {
    const user = userEvent.setup();
    renderActions();

    await user.click(await screen.findByRole("button", { name: /rotate ssh key for prod-1/i }));
    await user.click(await screen.findByRole("button", { name: /^rotate key$/i }));

    await waitFor(() => expect(m.rotateManagementKey).toHaveBeenCalledWith({ serverId: "srv-1" }));
  });

  it("revokes SSH access after confirmation", async () => {
    const user = userEvent.setup();
    renderActions();

    await user.click(await screen.findByRole("button", { name: /revoke ssh access for prod-1/i }));
    await user.click(await screen.findByRole("button", { name: /^revoke access$/i }));

    await waitFor(() => expect(m.revokeManagementKey).toHaveBeenCalledWith({ serverId: "srv-1" }));
  });

  it("hides rotate/revoke when the server has no managed key", async () => {
    m.getManagementKey.mockResolvedValue({ key: null });
    renderActions();

    expect(await screen.findByRole("button", { name: /install command/i })).toBeInTheDocument();
    await waitFor(() => expect(m.getManagementKey).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: /rotate ssh key/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /revoke ssh access/i })).not.toBeInTheDocument();
  });

  it("hides rotate/revoke when the managed key is already revoked", async () => {
    m.getManagementKey.mockResolvedValue({ key: key("2026-01-01T00:00:00Z") });
    renderActions();

    expect(await screen.findByRole("button", { name: /install command/i })).toBeInTheDocument();
    await waitFor(() => expect(m.getManagementKey).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: /rotate ssh key/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /revoke ssh access/i })).not.toBeInTheDocument();
  });
});
