import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";

import { AdminMcpSetup } from "./AdminMcpSetup";
import { renderWithApp } from "@/test/harness";

afterEach(cleanup);

describe("Admin MCP setup", () => {
  it("uses the current private dashboard origin for each install option", async () => {
    await renderWithApp(<AdminMcpSetup />);
    const endpoint = `${window.location.origin}/admin-mcp`;

    expect(
      screen.getByRole("heading", { name: "Connect Admin MCP" }),
    ).toBeTruthy();
    expect(screen.getByText(endpoint)).toBeTruthy();
    expect(
      screen.getByText(
        `claude mcp add --transport http --scope user gram-admin ${endpoint}`,
      ),
    ).toBeTruthy();

    fireEvent.mouseDown(screen.getByRole("tab", { name: "Codex" }), {
      button: 0,
    });
    expect(
      await screen.findByText(`codex mcp add gram-admin --url ${endpoint}`),
    ).toBeTruthy();

    fireEvent.mouseDown(screen.getByRole("tab", { name: "Cursor" }), {
      button: 0,
    });
    expect(
      screen.getByRole("button", { name: "Copy Cursor configuration" })
        .previousElementSibling?.textContent,
    ).toBe(
      JSON.stringify(
        { mcpServers: { "gram-admin": { url: endpoint } } },
        null,
        2,
      ),
    );

    fireEvent.mouseDown(screen.getByRole("tab", { name: "Other agents" }), {
      button: 0,
    });
    expect(await screen.findByText(/dynamic client registration/)).toBeTruthy();
  });
});
