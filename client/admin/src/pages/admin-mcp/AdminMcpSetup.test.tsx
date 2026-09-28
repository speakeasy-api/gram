import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";

import { AdminMcpSetup } from "./AdminMcpSetup";
import { renderWithApp } from "@/test/harness";

const browser = window as typeof window & {
  happyDOM: { setURL(url: string): void };
};

afterEach(() => {
  cleanup();
  browser.happyDOM.setURL("http://localhost:3000");
});

describe("Admin MCP setup", () => {
  it("uses the current private dashboard origin for each install option", async () => {
    browser.happyDOM.setURL("https://admin.example.test/mcp-setup");
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
      screen.getByText(
        JSON.stringify(
          { mcpServers: { "gram-admin": { type: "http", url: endpoint } } },
          null,
          2,
        ),
        { normalizer: (text) => text },
      ),
    ).toBeTruthy();

    fireEvent.mouseDown(screen.getByRole("tab", { name: "Other agents" }), {
      button: 0,
    });
    expect(await screen.findByText(/dynamic client registration/)).toBeTruthy();
  });

  it("does not generate install instructions over plain HTTP", async () => {
    browser.happyDOM.setURL("http://localhost:5174/mcp-setup");
    await renderWithApp(<AdminMcpSetup />);

    expect(
      screen.getByText(/private HTTPS Tailscale dashboard address/),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Copy Admin MCP URL" }),
    ).toBeNull();
    expect(screen.queryByText("http://localhost:5174/admin-mcp")).toBeNull();
  });
});
