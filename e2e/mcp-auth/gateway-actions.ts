import { test, expect, type AuthScenario } from "./fixtures.ts";

export type StoredGateway = { name: string; url: string; endpoint: string };

/** Stored meta MCP servers, not the automatic organization/project gateway. */
export class GatewayActions {
  constructor(
    private readonly auth: Pick<AuthScenario, "page" | "dashboard">,
  ) {}

  async projectEndpoint() {
    const { dashboard } = this.auth;
    const organizationEndpoint = await dashboard.organizationGateway();
    const projectSlug = new URL(dashboard.projectURL).pathname
      .split("/")
      .filter(Boolean)
      .at(-1)!;
    // There is no separate project-wide gateway editor. This is the canonical
    // project-scoped route of the UI-enabled organization gateway, asserted in
    // server/internal/mcp/serve_project_gateway_internal_test.go. It is NOT a
    // stored meta MCP server and must never stand in for one in these tests.
    return `${organizationEndpoint.replace(/\/$/, "")}/projects/${encodeURIComponent(projectSlug)}`;
  }

  async create(name: string, members: string[]): Promise<StoredGateway> {
    return test.step(
      `UI stored gateway setup: ${name}`,
      async () => {
        const { page, dashboard } = this.auth;
        // AddMcpServer -> InputDialog -> GatewayDetails overview.
        await page.goto(`${dashboard.projectURL}/mcp/add`);
        await page.getByText("New gateway", { exact: true }).click();
        const dialog = page.getByRole("dialog", { name: "Create Gateway" });
        await dialog.getByPlaceholder("My Gateway", { exact: true }).fill(name);
        await dialog
          .getByRole("button", { name: "Create", exact: true })
          .click();
        await page.waitForURL(/\/mcp\/gateway\/[^/]+\/overview$/);
        const url = page.url().replace(/\/overview$/, "");
        await expect(
          page.getByRole("heading", { name, exact: true }),
        ).toBeVisible();
        await this.add({ url }, members);
        // Stay in the mounted app. Full navigation restarts Vite and the assistant
        // shell; the retained timeout stalled during that bootstrap, before auth.
        await page.getByRole("link", { name: "Settings", exact: true }).click();
        await page.waitForURL(`${url}/settings`);
        await expect(
          page.getByRole("heading", { name: "Gateway URL", exact: true }),
          "UI_STORED_GATEWAY_SETUP_BLOCKER: stored gateway settings must expose its own address",
        ).toBeVisible();
        // Creation provisions the gateway's own project issuer. Keep that real
        // UI-selected policy; an unrelated organization issuer may not be eligible.
        const issuer = page.getByRole("combobox", {
          name: "User session issuer",
          exact: true,
        });
        await expect(issuer).toBeVisible();
        await expect(
          issuer,
          "UI_STORED_GATEWAY_SETUP_BLOCKER: stored gateway must have a dashboard-provisioned sign-in policy",
        ).toContainText(/ — (?:Organization|Project)/);
        const selectedIssuer = (await issuer.innerText()).trim();
        await page.reload();
        // Separate fresh-page readiness from persisted issuer identity. Keep the
        // exact issuer assertion below; never select a replacement on reload.
        await expect(
          page.getByRole("heading", { name: "Gateway URL", exact: true }),
          "UI_STORED_GATEWAY_RELOAD_BLOCKER: settings must mount after reload",
        ).toBeVisible({ timeout: 60_000 });
        await expect(
          page.getByRole("combobox", {
            name: "User session issuer",
            exact: true,
          }),
        ).toHaveText(selectedIssuer);
        // GatewaySettingsTab uses the same ServerUrlSection as direct servers.
        const endpoint = await dashboard.endpoint("gateway");
        return { name, url, endpoint };
      },
      { timeout: 90_000 },
    );
  }

  async add(gateway: Pick<StoredGateway, "url">, members: string[]) {
    const { page } = this.auth;
    await page.goto(`${gateway.url}/overview`);
    await page
      .getByRole("button", { name: "Add servers", exact: true })
      // Header and empty-state buttons open the same member picker.
      .first()
      .click();
    const picker = page.getByRole("dialog", { name: "Add servers to gateway" });
    for (const name of members) {
      const checkbox = picker.getByRole("checkbox", { name, exact: true });
      await expect(
        checkbox,
        `UI_MEMBERSHIP_SETUP_BLOCKER: ${name} must be selectable by name`,
      ).toBeEnabled();
      await checkbox.check();
    }
    await picker
      .getByRole("button", { name: "Add selected servers", exact: true })
      .click();
    await expect(picker).not.toBeVisible();
    for (const name of members) await expect(this.row(name)).toBeVisible();
  }

  private row(name: string) {
    return this.auth.page.getByRole("row").filter({
      has: this.auth.page.getByRole("link", { name, exact: true }),
    });
  }

  async remove(gateway: StoredGateway, member: string) {
    const { page } = this.auth;
    await page.goto(`${gateway.url}/overview`);
    await this.row(member)
      .getByRole("button", { name: "Remove server", exact: true })
      .click();
    const dialog = page.getByRole("dialog", { name: "Remove this server?" });
    await expect(dialog).toContainText(member);
    await dialog
      .getByRole("button", { name: "Remove server", exact: true })
      .click();
    await expect(dialog).not.toBeVisible();
    await expect(this.row(member)).toHaveCount(0);
  }
}
