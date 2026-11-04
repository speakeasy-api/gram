import type { Page } from "@playwright/test";
import { expect, type AuthScenario } from "./fixtures.ts";

/** Real managed-assistant turns, not the client-side onboarding preview. */
export class AssistantAuthActions {
  private settingsURL = "";
  constructor(private readonly auth: AuthScenario) {}

  static requireModel() {
    if (!process.env.OPENROUTER_DEV_KEY && !process.env.OPENROUTER_API_KEY) {
      throw new Error(
        "ENV_ASSISTANT_MODEL: configure a real OpenRouter key for the local assistant runtime (OPENROUTER_DEV_KEY); no mocked completion or tool execution is accepted",
      );
    }
  }

  async attach(serverSlug: string) {
    const { page, dashboard } = this.auth;
    // Project creation already calls ensureManagedAssistant. Do not create a
    // second assistant through an LLM-driven onboarding conversation.
    await page.goto(`${dashboard.projectURL}/assistants`);
    await page
      .getByRole("link")
      .filter({ hasText: /Project Assistant for / })
      .click();
    this.settingsURL = page.url();
    const section = page.getByText(/^MCP Servers \(\d+\)$/).locator("..");
    await section.getByRole("button", { name: "Add", exact: true }).click();
    // The dialog title is rendered as a heading, not its accessible name.
    const dialog = page.getByRole("dialog").filter({
      has: page.getByRole("heading", {
        name: "Attach MCP Servers",
        exact: true,
      }),
    });
    await dialog
      .getByRole("checkbox", { name: serverSlug, exact: true })
      .check();
    await dialog.getByRole("button", { name: "Attach", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(
      page.getByRole("link", { name: serverSlug, exact: true }),
    ).toBeVisible();
    await this.identity();
  }

  async identity() {
    await this.auth.page
      .getByRole("tab", { name: "Identity", exact: true })
      .click();
    await expect(
      this.auth.page.getByText("Upstream credentials", { exact: true }),
    ).toBeVisible();
  }

  async settings() {
    await this.auth.page.goto(this.settingsURL);
    await this.identity();
  }

  profile(serverName: string) {
    const { page } = this.auth;
    // A root dashboard turn acts for a person. Never change a userless trigger
    // or the unrelated server-settings "Choose for an app" issuance profile.
    return page
      .getByText("When acting for a person", { exact: true })
      .locator("../..")
      .getByRole("combobox", {
        name: `Credential for ${serverName}`,
        exact: true,
      });
  }

  async choose(serverName: string, credential: string) {
    const control = this.profile(serverName);
    await expect(control).toHaveCount(1);
    await control.click();
    await this.auth.page
      .getByRole("option")
      .filter({ hasText: `${credential} · ` })
      .click();
    await expect(control).toContainText(credential);
    await this.auth.page.reload();
    await this.identity();
    await expect(this.profile(serverName)).toContainText(credential);
  }

  async openChat() {
    const { page } = this.auth;
    await page
      .getByRole("link", { name: "Project Assistant Beta", exact: true })
      .click();
    await expect(
      page
        .getByRole("textbox", { name: "Message input", exact: true })
        .filter({ visible: true }),
    ).toHaveCount(1);
  }

  async send(serverSlug: string) {
    const { page } = this.auth;
    await page
      .getByRole("textbox", { name: "Message input", exact: true })
      .filter({ visible: true })
      .fill(
        `Call the fixture_identity tool on MCP server ${serverSlug} now. Do not infer or invent an identity. Return the actual tool result. If authentication is required, show its authorization link.`,
      );
    await page
      .getByRole("button", { name: "Send message", exact: true })
      .filter({ visible: true })
      .click();
  }

  async executedSince(offset: number, resource: string, principal: string) {
    await expect
      .poll(
        () =>
          this.auth.mock.events
            .slice(offset)
            .filter(
              (event) =>
                event.kind === "mcp" &&
                event.method === "tools/call" &&
                event.tool === "fixture_identity" &&
                event.resource === resource &&
                event.principal === principal &&
                event.outcome === "authorized",
            ).length,
        {
          timeout: 90_000,
          message:
            "ASSISTANT_EXECUTION_BLOCKER: real runtime must call fixture_identity under the selected upstream identity",
        },
      )
      .toBeGreaterThan(0);
  }

  async authorize() {
    const { page } = this.auth;
    const link = page
      .locator('a[href*="assistantMcpAuth"][href*="code_challenge"]')
      .filter({ visible: true })
      .last();
    await expect(
      link,
      "ASSISTANT_OAUTH_BLOCKER: runtime must render its real authorization link",
    ).toBeVisible({ timeout: 90_000 });
    const href = await link.getAttribute("href");
    expect(href).toBeTruthy();
    const consent = await page.context().newPage();
    try {
      await consent.goto(new URL(href!, page.url()).href);
      await this.completeConsent(consent);
    } finally {
      await consent.close();
    }
  }

  private async completeConsent(page: Page) {
    const done = page.getByText(
      "Authentication complete. You can close this window.",
      { exact: true },
    );
    for (let step = 0; step < 12; step++) {
      const controls = [
        page.getByRole("button", { name: "Approve mock access", exact: true }),
        page.getByRole("button", { name: "Give access", exact: true }),
        page.getByRole("button", { name: "Log in", exact: true }),
        page.getByRole("link", { name: /^(Connect|Reconnect)(\s|$)/ }).first(),
        page
          .getByRole("button", { name: /^(Connect|Reconnect)(\s|$)/ })
          .first(),
      ];
      await expect
        .poll(
          async () =>
            (await done.isVisible()) ||
            (
              await Promise.all(
                controls.map(
                  async (control) =>
                    (await control.isVisible()) && (await control.isEnabled()),
                ),
              )
            ).some(Boolean),
          {
            timeout: 15_000,
            message:
              "ASSISTANT_OAUTH_BLOCKER: no actionable browser consent control",
          },
        )
        .toBe(true);
      if (await done.isVisible()) return;
      for (const control of controls) {
        if ((await control.isVisible()) && (await control.isEnabled())) {
          await control.click();
          break;
        }
      }
    }
    await expect(done).toBeVisible();
  }
}
