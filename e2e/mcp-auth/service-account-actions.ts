import { randomUUID } from "node:crypto";
import { expect, type AuthScenario } from "./fixtures.ts";

/** Provision only through the organization Authorization Servers and MCP UI. */
export class ServiceAccountActions {
  private issuerClientsURL = "";
  private registrationName = "";

  constructor(private readonly auth: AuthScenario) {}

  async registration() {
    const { page, dashboard, mock } = this.auth;
    const name = `auth-e2e-self-${randomUUID().slice(0, 8)}`;
    await page.goto(`${dashboard.projectURL}/remote-identity-providers`);
    // The assistant shell can remount a freshly created project's routes.
    // Wait for the rendered wrapper before opening an editable sheet.
    await page
      .locator(".gram-elements")
      .getByRole("button", { name: "New Authorization Server", exact: true })
      .click();
    const issuer = page.getByRole("dialog");
    await issuer
      .getByPlaceholder("https://login.example.com", { exact: true })
      .fill(mock.providers.alpha.issuer);
    await issuer
      .getByPlaceholder("my-authorization-server", { exact: true })
      .fill(name);
    await issuer
      .getByPlaceholder("My Authorization Server", { exact: true })
      .fill(name);
    // Manual issuer configuration deliberately proves that discovery and a
    // browser authorization endpoint are not required for client_credentials.
    await issuer
      .getByPlaceholder("https://login.example.com/token", { exact: true })
      .fill(`${mock.providers.alpha.issuer}/token`);
    await expect(issuer.getByRole("combobox")).toContainText(
      "Organizational (all projects)",
    );
    await issuer.getByRole("button", { name: "Create", exact: true }).click();
    await expect(issuer).not.toBeVisible();
    await page.getByRole("tab", { name: "Clients", exact: true }).click();
    this.issuerClientsURL = page.url();
    this.registrationName = name;
    await page.getByRole("button", { name: "New Client", exact: true }).click();
    const registration = page.getByRole("dialog", {
      name: "New Client",
      exact: true,
    });
    const self = registration.getByRole("radio", { name: /^Service account/ });
    await expect(
      self,
      "UI_SELF_REGISTRATION_BLOCKER: issuer must expose its token endpoint",
    ).toBeEnabled();
    await self.focus();
    await self.press("Space");
    await expect(self).toBeChecked();
    await expect(registration.getByRole("combobox")).toContainText(
      "All projects (organization-level)",
    );
    await registration
      .locator("#self-client-id")
      .fill(mock.providers.alpha.clientId);
    await registration
      .getByPlaceholder("Paste the client secret", { exact: true })
      .fill(mock.providers.alpha.clientSecret);
    await expect(
      registration.getByRole("radio", { name: /client_secret_basic/ }),
    ).toBeChecked();
    await registration
      .getByPlaceholder("read, write, openid", { exact: true })
      .fill("fixture:read");
    await registration
      .getByRole("button", { name: "Create", exact: true })
      .click();
    await expect(registration).not.toBeVisible();
    // A fixed provider client_id is shared by isolated mocks, so name this
    // registration explicitly rather than selecting a stale run's client_id.
    await page.getByTitle("Rename client", { exact: true }).click();
    const rename = page.getByRole("textbox", { name: "Name", exact: true });
    await rename.fill(name);
    await rename.press("Enter");
    await expect(rename).not.toBeVisible();
    await expect(
      page.getByTitle("Rename client", { exact: true }),
    ).toContainText(name);
    return name;
  }

  async cleanup() {
    const { page, dashboard } = this.auth;
    if (!this.registrationName.startsWith("auth-e2e-self-"))
      throw new Error("Refusing to remove a non-fixture registration");
    await page.goto(this.issuerClientsURL);
    const clientRow = page
      .getByRole("row")
      .filter({ has: page.getByText(this.registrationName, { exact: true }) });
    await expect(clientRow).toHaveCount(1);
    await clientRow.getByRole("button").click();
    await page
      .getByRole("menuitem", { name: "Delete client", exact: true })
      .click();
    const clientDialog = page.getByRole("dialog", {
      name: `Delete client "${this.registrationName}"?`,
      exact: true,
    });
    await clientDialog
      .getByRole("button", { name: "Delete client", exact: true })
      .click();
    await expect(clientDialog).not.toBeVisible();
    await expect(clientRow).toHaveCount(0);
    await page.goto(`${dashboard.projectURL}/remote-identity-providers`);
    const issuerRow = page
      .getByRole("row")
      .filter({ has: page.getByText(this.registrationName, { exact: true }) });
    await expect(issuerRow).toHaveCount(1);
    await issuerRow.getByRole("button").click();
    await page.getByRole("menuitem", { name: "Delete", exact: true }).click();
    const issuerDialog = page.getByRole("dialog", {
      name: `Delete "${this.registrationName}"?`,
      exact: true,
    });
    await issuerDialog
      .getByRole("button", { name: "Delete authorization server", exact: true })
      .click();
    await expect(issuerDialog).not.toBeVisible();
    await expect(issuerRow).toHaveCount(0);
  }

  async offer(serverURL: string, registrationName: string, label: string) {
    const { page } = this.auth;
    const name = `${label}-${randomUUID().slice(0, 8)}`;
    await page.goto(`${serverURL}/settings`);
    await page
      .getByRole("button", { name: "Add credential", exact: true })
      .click();
    const dialog = page.getByRole("dialog", {
      name: "Add a credential",
      exact: true,
    });
    const kind = dialog.locator("#add-credential-service_account");
    await kind.focus();
    await kind.press("Space");
    await expect(kind).toBeChecked();
    await dialog.getByLabel("Name", { exact: true }).fill(name);
    await dialog
      .getByRole("combobox", { name: "Sign-in", exact: true })
      .click();
    const option = page.getByRole("option", {
      name: registrationName,
      exact: true,
    });
    await expect(
      option,
      "UI_SELF_REGISTRATION_BLOCKER: exact self-owned registration must be selectable",
    ).toHaveCount(1);
    await option.click();
    await dialog
      .getByRole("button", { name: "Add credential", exact: true })
      .click();
    await expect(dialog).not.toBeVisible();
    await this.allowEveryone(name);
    return name;
  }

  async allowEveryone(name: string) {
    const { page } = this.auth;
    await page
      .getByRole("button")
      .filter({ has: page.getByText(name, { exact: true }) })
      .click();
    const sheet = page.getByRole("dialog");
    const audience = sheet.locator("section").filter({
      has: page.getByRole("heading", {
        name: "Who can use this",
        exact: true,
      }),
    });
    await audience.getByRole("button", { name: "Add", exact: true }).click();
    const picker = page.getByRole("dialog", {
      name: "Who else can use this credential?",
      exact: true,
    });
    await picker.getByRole("combobox").click();
    const everyone = page.getByRole("option", {
      name: /^Everyone, not selected/,
    });
    await expect(
      everyone,
      "UI_SELF_AUDIENCE_BLOCKER: a broader block must not be bypassed",
    ).toBeEnabled();
    await everyone.click();
    await page.keyboard.press("Escape");
    await picker.getByRole("button", { name: "Add", exact: true }).click();
    await expect(picker).not.toBeVisible();
    await audience.getByRole("button", { name: "Save", exact: true }).click();
    await expect(
      audience.getByRole("button", { name: "Save", exact: true }),
    ).not.toBeVisible();
    await expect(
      audience
        .getByRole("group", {
          name: "Access for Everyone in the organization",
          exact: true,
        })
        .getByRole("button", { name: "Can use", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    await page.keyboard.press("Escape");
    await expect(sheet).not.toBeVisible();
  }
}
