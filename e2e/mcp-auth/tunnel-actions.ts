import { spawn, execFile, type ChildProcess } from "node:child_process";
import { access, mkdtemp, rm } from "node:fs/promises";
import { constants } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { expect, type AuthScenario } from "./fixtures.ts";

const execute = promisify(execFile);
const repository = fileURLToPath(new URL("../../", import.meta.url));

export type TunneledSource = {
  name: string;
  serverURL: string;
  endpoint: string;
};

/** A real UI-created tunnel, driven by the repository's WebSocket/yamux agent. */
export class TunnelActions {
  private directory: string | undefined;
  private executable: string | undefined;
  private children: ChildProcess[] = [];

  constructor(private readonly auth: AuthScenario) {}

  private gatewayURL() {
    // The dashboard's public-host snippet is production-oriented. The local
    // gateway exposes its own public listener, configured by this worktree.
    const port = process.env.TUNNEL_GATEWAY_PUBLIC_PORT;
    const configured = process.env.MCP_AUTH_TUNNEL_GATEWAY_URL;
    if (!configured && (!port || !/^\d+$/.test(port))) {
      throw new Error(
        "ENV_TUNNEL_GATEWAY: set this worktree's TUNNEL_GATEWAY_PUBLIC_PORT or MCP_AUTH_TUNNEL_GATEWAY_URL; no daemon is started by this test",
      );
    }
    let url: URL;
    try {
      url = new URL(configured ?? `ws://127.0.0.1:${port}/connect`);
    } catch {
      throw new Error("ENV_TUNNEL_GATEWAY: invalid local connector URL");
    }
    if (
      !["127.0.0.1", "localhost", "[::1]"].includes(url.hostname) ||
      !["ws:", "wss:"].includes(url.protocol) ||
      url.username ||
      url.password ||
      url.pathname !== "/connect" ||
      url.search ||
      url.hash
    ) {
      throw new Error(
        "ENV_TUNNEL_GATEWAY: require a credential-free loopback ws(s) /connect URL",
      );
    }
    return url.href;
  }

  private async binary() {
    if (this.executable) return this.executable;
    const configured = process.env.MCP_AUTH_TUNNEL_AGENT_BIN;
    if (configured) {
      const path = resolve(repository, configured);
      try {
        await access(path, constants.X_OK);
      } catch {
        throw new Error(
          "ENV_TUNNEL_AGENT: MCP_AUTH_TUNNEL_AGENT_BIN must name an executable native agent",
        );
      }
      this.executable = path;
      return path;
    }
    // No dedicated build:tunnel-agent task exists. Use the repository's mise
    // toolchain and compile its actual command, never a substitute transport.
    this.directory = await mkdtemp(join(tmpdir(), "mcp-auth-tunnel-"));
    const path = join(this.directory, "tunnel-agent");
    try {
      await execute(
        "mise",
        [
          "exec",
          "--",
          "go",
          "build",
          "-mod=readonly",
          "-trimpath",
          "-o",
          path,
          "./tunnel/cmd/tunnel-agent",
        ],
        {
          cwd: repository,
          timeout: 120_000,
          maxBuffer: 128 * 1024,
        },
      );
    } catch {
      throw new Error(
        "ENV_TUNNEL_AGENT_BUILD: native tunnel/cmd/tunnel-agent could not build with mise go -mod=readonly; provide MCP_AUTH_TUNNEL_AGENT_BIN or repair the Go toolchain",
      );
    }
    this.executable = path;
    return path;
  }

  private async connect(
    key: string,
    upstream: string,
    executable: string,
    gateway: string,
  ) {
    const target = new URL(upstream);
    if (
      !["127.0.0.1", "localhost", "[::1]"].includes(target.hostname) ||
      target.username ||
      target.password
    ) {
      throw new Error(
        "ENV_TUNNEL_TARGET: the connector must target the standalone loopback fixture",
      );
    }
    // Do not inherit database, management, provider, or dashboard credentials.
    // In particular, never put the one-time UI key in argv, journals or errors.
    const child = spawn(executable, [], {
      cwd: repository,
      env: {
        TUNNEL_GATEWAY_URL: gateway,
        TUNNEL_KEY: key,
        TUNNEL_LOCAL_MCP_URL: upstream,
        TUNNEL_SERVICE_VERSION: "auth-e2e-native-v1",
        TUNNEL_DISABLE_DIAGNOSTICS: "1",
      },
      stdio: ["ignore", "pipe", "pipe"],
    });
    this.children.push(child);
    let connected = false;
    let failed = false;
    let pending = "";
    child.on("error", () => {
      failed = true;
    });
    child.on("exit", () => {
      failed = true;
    });
    // Parse only the stable, non-secret lifecycle message. Raw native logs are
    // discarded; even an unexpected gateway error must not leak credentials.
    child.stderr?.resume();
    child.stdout?.on("data", (chunk: Buffer) => {
      pending += chunk.toString("utf8");
      const lines = pending.split("\n");
      pending = lines.pop() ?? "";
      if (pending.length > 16_384) pending = "";
      for (const line of lines) {
        try {
          const message: unknown = JSON.parse(line);
          if (
            typeof message === "object" &&
            message !== null &&
            "msg" in message &&
            message.msg === "tunnel agent connected"
          )
            connected = true;
        } catch {
          /* Native output is not evidence until it is a valid lifecycle event. */
        }
      }
    });
    await expect
      .poll(() => connected && !failed, {
        timeout: 30_000,
        message:
          "ENV_TUNNEL_HANDSHAKE: native agent must authenticate the UI-issued key over the local gateway /connect WebSocket; check the existing gateway, database/Redis wiring and TLS trust without restarting the stack",
      })
      .toBe(true);
  }

  async create(
    name: string,
    upstream: string,
    resourceIdentifier?: string,
  ): Promise<TunneledSource> {
    const { page, dashboard } = this.auth;
    const gateway = this.gatewayURL();
    const executable = await this.binary();
    await page.goto(`${dashboard.projectURL}/mcp/add/tunneled`);
    const issuer = page.locator(".gram-elements").getByRole("combobox", {
      name: "User session issuer",
      exact: true,
    });
    await expect(
      issuer,
      "UI_TUNNEL_ISSUER_BLOCKER: wait for persisted issuer defaults",
    ).toContainText(/Organization|Project|project-specific/);
    await page.locator("#tunneled-mcp-name").fill(name);
    const resource = page.locator("#tunneled-mcp-resource-identifier");
    if (resourceIdentifier) await resource.fill(resourceIdentifier);
    await expect(resource).toHaveValue(resourceIdentifier ?? "");
    await expect(
      page.locator("#tunneled-mcp-name"),
      "UI_TUNNEL_FORM_BLOCKER: the ready shell must retain the entered name",
    ).toHaveValue(name);
    const add = page.getByRole("button", { name: "Add server", exact: true });
    await expect(
      add,
      "UI_TUNNEL_FORM_BLOCKER: valid tunnel form must be submittable",
    ).toBeEnabled();
    await add.click();
    const open = page.getByRole("button", {
      name: "Open MCP server",
      exact: true,
    });
    await expect(
      open,
      "UI_TUNNEL_CREATE_BLOCKER: dashboard must create the tunnel and issue its one-time credential",
    ).toBeVisible();
    // Read only the dedicated key card, never the example snippets (which also
    // contain the key). This value is kept in process memory and never returned.
    const card = page
      .locator("div.border.p-5")
      .filter({ has: page.getByText("Tunnel key", { exact: true }) });
    const key = (await card.locator("pre").textContent())?.trim();
    if (!key)
      throw new Error(
        "UI_TUNNEL_KEY_BLOCKER: creation must expose its one-time tunnel credential",
      );
    await this.connect(key, upstream, executable, gateway);
    await open.click();
    await page.waitForURL(/\/mcp\/[^/]+\/overview$/);
    const serverURL = page.url().replace(/\/overview$/, "");
    await expect(
      page.getByText(
        "1 tunnel agent connected. Checks are turned off, so reachability is not reported.",
        { exact: true },
      ),
      "ENV_TUNNEL_REGISTRATION: the dashboard must observe the authenticated native connection",
    ).toBeVisible({ timeout: 60_000 });
    // A newly created tunnel starts offline. A hosted address and a connected
    // native agent do not enable serving; choose authenticated Private access.
    const disabled = page.getByRole("button", {
      name: "Disabled",
      exact: true,
    });
    const serving = page.getByRole("button", { name: "Private", exact: true });
    await expect(disabled.or(serving)).toBeVisible();
    if (await disabled.isVisible()) {
      await disabled.click();
      await page.getByRole("menuitem", { name: /^Private/ }).click();
    }
    await expect(
      serving,
      "UI_TUNNEL_SERVING_BLOCKER: tunnel must serve authenticated downstream clients",
    ).toBeVisible();
    await page.getByRole("link", { name: "Settings", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Server URL", exact: true }),
    ).toBeVisible();
    await expect(serving).toBeVisible();
    const graph = await this.auth.graph();
    expect(
      graph.servers.find((server) => server.name === name),
      "FIXTURE_MODEL_MISMATCH: tunnel availability must persist before discovery",
    ).toMatchObject({
      kind: "tunneled",
      visibility: "private",
      disabled: false,
    });
    return { name, serverURL, endpoint: await dashboard.endpoint() };
  }

  async close() {
    for (const child of this.children.splice(0)) {
      if (!child.pid || child.exitCode !== null || child.signalCode !== null)
        continue;
      await new Promise<void>((resolveClose) => {
        const timeout = setTimeout(() => child.kill("SIGKILL"), 5_000);
        child.once("close", () => {
          clearTimeout(timeout);
          resolveClose();
        });
        child.kill("SIGTERM");
      });
    }
    if (this.directory)
      await rm(this.directory, { recursive: true, force: true });
  }
}
