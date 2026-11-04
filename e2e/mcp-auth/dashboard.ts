import { expect, type Page } from "@playwright/test";

/** All product provisioning is deliberately driven by visible dashboard controls. */
export class Dashboard {
  projectURL = "";
  private projectName = "";
  readonly createdProjects: { name: string; url: string }[] = [];
  organizationSlug = "";
  private changedEnforcement = false;
  private changedGateway = false;
  constructor(
    readonly page: Page,
    readonly site = process.env.GRAM_SITE_URL ?? "",
  ) {
    if (!site)
      throw new Error(
        "UI_PREREQUISITE: GRAM_SITE_URL must identify the local dashboard",
      );
  }

  async login() {
    await this.page.goto(this.site);
    const login = this.page.getByRole("button", {
      name: "Log in",
      exact: true,
    });
    const add = this.page.getByRole("button", { name: "Add New", exact: true });
    const home = this.page.getByRole("link", { name: "Home", exact: true });
    await expect(login.or(add).or(home).first()).toBeVisible();
    if (await login.isVisible()) await login.click();
    await expect(
      add.or(home).first(),
      "UI_LOGIN_BLOCKER: real dev-idp login must return to the dashboard",
    ).toBeVisible();
    // Root may restore the user's last project. Follow the rendered Home link
    // rather than assuming every authenticated landing page has Add New.
    if (!(await add.isVisible())) await home.click();
    await expect(
      add,
      "UI_LOGIN_BLOCKER: organization home must offer project creation",
    ).toBeVisible();
    this.organizationSlug = new URL(this.page.url()).pathname
      .split("/")
      .filter(Boolean)[0]!;
  }

  async createProject(name: string) {
    this.projectName = name;
    await this.page
      .getByRole("button", { name: "Add New", exact: true })
      .click();
    await this.page
      .getByRole("menuitem", { name: "Project", exact: true })
      .click();
    const dialog = this.page.getByRole("dialog");
    await dialog.getByPlaceholder("My Project", { exact: true }).fill(name);
    await dialog
      .getByRole("button", { name: "Create Project", exact: true })
      .click();
    await this.page.waitForURL((url) =>
      url.pathname.includes(`/projects/${name}`),
    );
    this.projectURL = this.page.url().replace(/\/$/, "");
    this.createdProjects.push({ name, url: this.projectURL });
    await expect(
      this.page.getByRole("button", { name: `${name} Project`, exact: true }),
    ).toBeVisible();
  }

  async removeProject() {
    const project = this.createdProjects.find(
      (value) => value.url === this.projectURL,
    );
    if (!project || !project.name.startsWith("auth-e2e-"))
      throw new Error("Refusing to delete a non-fixture project");
    await this.page.goto(`${this.projectURL}/settings`);
    await this.page
      .getByRole("button", { name: "Delete Project", exact: true })
      .click();
    const dialog = this.page.getByRole("dialog");
    await dialog
      .getByLabel("Type the project name to confirm:", { exact: true })
      .fill(project.name);
    await dialog
      .getByRole("button", { name: "Delete Project", exact: true })
      .click();
    await expect(dialog).not.toBeVisible();
    this.createdProjects.splice(this.createdProjects.indexOf(project), 1);
  }

  async removeCreatedProjects() {
    for (const project of [...this.createdProjects].reverse()) {
      this.projectURL = project.url;
      await this.removeProject();
    }
  }

  async remote(
    name: string,
    upstream: string,
    identity: "user" | "none" = "none",
    projectIssuer = false,
  ) {
    await this.page.goto(`${this.projectURL}/mcp/add/remote`);
    // The project assistant inserts this wrapper when ready, remounting route
    // children. Wait before entering data so fixture setup is not erased.
    await expect(
      this.page.locator(".gram-elements").getByLabel("Display name (optional)"),
      "UI_RUNTIME_READY_BLOCKER: wait for the assistant-enabled project shell before entering form data",
    ).toBeVisible();

    const issuerSelection = this.page.getByRole("combobox", {
      name: "User session issuer",
      exact: true,
    });
    await expect(
      issuerSelection,
      "UI_FORM_READY_BLOCKER: wait for issuer defaults before entering the new server",
    ).toContainText(/Organization|Project|project-specific/);
    if (projectIssuer) {
      const issuer = this.page.getByRole("combobox", {
        name: "User session issuer",
        exact: true,
      });
      await issuer.click();
      await this.page
        .getByRole("option", {
          name: "Create a project-specific issuer",
          exact: true,
        })
        .click();
      await expect(issuer).toHaveText("Create a project-specific issuer");
    }
    await this.page.getByLabel("Display name (optional)").fill(name);
    await this.page
      .getByLabel("MCP server URL", { exact: true })
      .fill(upstream);
    await expect(this.page.getByLabel("Display name (optional)")).toHaveValue(
      name,
    );
    await expect(
      this.page.getByLabel("MCP server URL", { exact: true }),
    ).toHaveValue(upstream);
    await this.page
      .getByRole("button", { name: "Verify connectivity", exact: true })
      .click();
    const save = this.page.getByRole("button", { name: "Save", exact: true });
    await expect(
      save,
      "UI_CONNECTIVITY_BLOCKER: dashboard could not verify the local fixture; check loopback access and the displayed verification error",
    ).toBeVisible({ timeout: 30_000 });
    // RadioCard puts its glyph over the native radio; keyboard activation uses its real control.
    const radio = this.page.getByRole("radio", {
      name: identity === "user" ? /^User Identity/ : /^Manual/,
    });
    await radio.focus();
    await radio.press("Space");
    await expect(radio).toBeChecked();
    await expect(
      save,
      "UI_IDENTITY_BLOCKER: the selected identity requires a dashboard-configured issuer/registration",
    ).toBeEnabled();
    await save.click();
    await this.page.waitForURL((url) =>
      /\/mcp\/[^/]+\/(overview|settings)$/.test(url.pathname),
    );
    const serverURL = this.page
      .url()
      .replace(/\/(overview|settings)(?:#.*)?$/, "");
    if (
      new URL(this.page.url()).pathname !==
      new URL(`${serverURL}/settings`).pathname
    ) {
      await this.page
        .getByRole("link", { name: "Settings", exact: true })
        .click();
    }
    await expect(
      this.page.getByRole("heading", { name: "Server URL", exact: true }),
      "UI_SETTINGS_RENDER_BLOCKER: Settings must render before discovering a hosted address",
    ).toBeVisible();
    if (projectIssuer) {
      const issuer = this.page.getByRole("combobox", {
        name: "User session issuer",
        exact: true,
      });
      await expect(
        issuer,
        "UI_ISSUER_BINDING_BLOCKER: project-specific selection must persist instead of using the Organization issuer",
      ).not.toContainText("Organization");
      await expect(issuer).toBeVisible();
    }
    return { name, serverURL, endpoint: await this.endpoint() };
  }

  async endpoint(subject: "server" | "gateway" = "server") {
    const address = this.page.locator("section").filter({
      has: this.page.getByRole("heading", {
        name: subject === "gateway" ? "Gateway URL" : "Server URL",
        exact: true,
      }),
    });
    const suffix = this.page
      .getByPlaceholder("endpoint", { exact: true })
      .first();
    const empty = address
      .getByText("No hosted address", { exact: true })
      .first();
    await expect(suffix.or(empty)).toBeVisible();
    if (!(await suffix.isVisible()) && (await empty.isVisible())) {
      await address
        .getByRole("button", { name: "Add", exact: true })
        .first()
        .click();
      const input = this.page.getByPlaceholder("my-endpoint", { exact: true });
      await input.fill(`fixture-${Date.now().toString(36)}`);
      await this.page
        .getByRole("button", { name: "Add address", exact: true })
        .click();
      await expect(input).not.toBeVisible();
    }
    await expect(
      suffix,
      "UI_ENDPOINT_BLOCKER: saved server must expose its Hosted Address in Settings",
    ).toBeVisible();
    // Both prefix and editable suffix are rendered in ServerUrlSection's InputGroup.
    const prefix = await suffix.locator("..").innerText();
    const endpoint = `${prefix.trim()}${await suffix.inputValue()}`;
    expect(endpoint).toMatch(/^https?:\/\/.+\/mcp\/.+/);
    return endpoint;
  }

  async staticHeader() {
    await this.configureTypedHeader(
      "X-Fixture-Key",
      "static",
      "fixture-static-secret",
    );
  }

  async personalStaticHeader(registrationLabel: string) {
    // Mirrors are intentionally read-only. Add an administrator-owned personal
    // offer for the exact mock registration, then edit that offer, not its mirror.
    const mirror = this.page.getByRole("button", {
      name: /Each person's own account/,
    });
    await expect(mirror).toHaveCount(1);
    const originalName = await mirror
      .locator("span[title]")
      .getAttribute("title");
    expect(originalName).toBeTruthy();
    const name = "fixture-personal-with-header";
    await this.page
      .getByRole("button", { name: "Add credential", exact: true })
      .click();
    const dialog = this.page.getByRole("dialog");
    const kind = dialog.locator("#add-credential-remote_session");
    await kind.focus();
    await kind.press("Space");
    await expect(kind).toBeChecked();
    await dialog.locator("#credential-name").fill(name);
    await dialog
      .getByRole("combobox", { name: "Sign-in", exact: true })
      .click();
    const option = this.page.getByRole("option", {
      name: registrationLabel,
      exact: true,
    });
    await expect(
      option,
      "UI_REGISTRATION_BLOCKER: exact fixture sign-in must be available",
    ).toHaveCount(1);
    await option.click();
    await dialog
      .getByRole("button", { name: "Add credential", exact: true })
      .click();
    await expect(dialog).not.toBeVisible();
    await this.configureTypedHeader(
      "X-Fixture-Key",
      "static",
      "fixture-static-secret",
      name,
    );
    const turnOff = this.page.getByRole("switch", {
      name: `Turn off ${originalName}`,
      exact: true,
    });
    await turnOff.click();
    const confirmation = this.page
      .getByRole("dialog")
      .getByRole("button", { name: "Turn off", exact: true });
    if (await confirmation.isVisible()) await confirmation.click();
    await expect(
      this.page.getByRole("switch", {
        name: `Turn on ${originalName}`,
        exact: true,
      }),
    ).not.toBeChecked();
  }

  async forwardedHeader() {
    await this.configureTypedHeader(
      "X-Fixture-User",
      "request",
      "X-Client-Fixture",
    );
  }

  private async configureTypedHeader(
    name: string,
    source: "static" | "request",
    value: string,
    credentialName?: string,
  ) {
    await expect(
      this.page.getByRole("button", { name: "Add credential", exact: true }),
      "UI_TYPED_CREDENTIAL_BLOCKER: enforced scenarios require the typed credential controls",
    ).toBeVisible();
    if (credentialName) {
      const personal = this.page.getByRole("button").filter({
        has: this.page.getByText(credentialName, { exact: true }),
      });
      await expect(personal).toHaveCount(1);
      await personal.click();
      const dialog = this.page.getByRole("dialog");
      const add = dialog.getByRole("button", {
        name:
          source === "static"
            ? "Add stored header"
            : "Add header from the caller",
        exact: true,
      });
      await expect(
        add,
        "UI_AUXILIARY_HEADER_BLOCKER: selected personal credential must expose editable headers; mirrored credentials are read-only",
      ).toBeVisible();
      await add.click();
      await dialog
        .getByRole("textbox", { name: "Header name", exact: true })
        .last()
        .fill(name);
      await dialog
        .getByRole("textbox", {
          name:
            source === "static"
              ? `Value for ${name}`
              : `Caller header for ${name}`,
          exact: true,
        })
        .last()
        .fill(value);
      const section = dialog.locator("section").filter({
        has: this.page.getByRole("heading", {
          name: "Headers sent upstream",
          exact: true,
        }),
      });
      const save = section.getByRole("button", { name: "Save", exact: true });
      await save.click();
      const confirm = this.page
        .getByRole("dialog")
        .getByRole("button", { name: "Save on every server", exact: true });
      if (await confirm.isVisible()) await confirm.click();
      await expect(save).not.toBeVisible();
      await dialog.getByRole("button", { name: "Close", exact: true }).click();
      await personal.click();
      await expect(
        this.page
          .getByRole("dialog")
          .getByRole("textbox", { name: "Header name", exact: true })
          .last(),
      ).toHaveValue(name);
      await this.page
        .getByRole("dialog")
        .getByRole("button", { name: "Close", exact: true })
        .click();
      return;
    }
    await this.addHeaderOffer(
      source === "static" ? "fixture-static" : "fixture-forwarded",
      source,
      name,
      value,
    );
    const enabled = this.page.getByRole("switch", {
      name: "Enable MCP server",
      exact: true,
    });
    await expect(enabled).toBeVisible();
    if (!(await enabled.isChecked())) await enabled.check();
    await expect(
      enabled,
      "UI_ENABLE_BLOCKER: a configured typed credential must allow enabling the connection",
    ).toBeChecked();
  }

  async personalCredentialName() {
    const personal = this.page.getByRole("button", {
      name: /Each person's own account/,
    });
    await expect(personal).toHaveCount(1);
    const name = await personal.locator("span[title]").getAttribute("title");
    if (!name)
      throw new Error(
        "UI_TYPED_CREDENTIAL_BLOCKER: personal credential must have a visible name",
      );
    return name;
  }

  async attachExistingOffer(
    name: string,
    sourceServer: string,
    targetServer: string,
  ) {
    await this.page
      .getByRole("button", { name: "Add credential", exact: true })
      .click();
    const dialog = this.page.getByRole("dialog");
    const existing = dialog.getByRole("radio", { name: /Existing credential/ });
    await existing.focus();
    await existing.press("Space");
    await expect(existing).toBeChecked();
    await dialog
      .getByRole("combobox", { name: "Credential", exact: true })
      .click();
    const option = this.page
      .getByRole("option")
      .filter({ hasText: name })
      .filter({ hasText: `Used on ${sourceServer}` });
    await expect(
      option,
      "UI_SHARED_CREDENTIAL_BLOCKER: select the exact existing source-server credential, never a clone",
    ).toHaveCount(1);
    await option.click();
    await dialog
      .getByRole("button", { name: "Add credential", exact: true })
      .click();
    await expect(dialog).not.toBeVisible();
    const shared = this.page
      .getByRole("button")
      .filter({ has: this.page.getByText(name, { exact: true }) });
    await expect(shared).toBeVisible();
    await shared.click();
    const usedOn = this.page
      .getByRole("dialog")
      .locator("section")
      .filter({
        has: this.page.getByRole("heading", { name: "Used on", exact: true }),
      });
    await expect(
      usedOn.getByRole("listitem"),
      "UI_SHARED_GRAPH_BLOCKER: one credential must be attached to exactly the source and target",
    ).toHaveCount(2);
    await expect(usedOn).toContainText(sourceServer);
    await expect(usedOn).toContainText(targetServer);
    await this.page
      .getByRole("dialog")
      .getByRole("button", { name: "Close", exact: true })
      .click();
    const enabled = this.page.getByRole("switch", {
      name: "Enable MCP server",
      exact: true,
    });
    await expect(enabled).toBeVisible();
    if (!(await enabled.isChecked())) await enabled.check();
    await expect(enabled).toBeChecked();
  }

  async addStaticOffer(name: string) {
    await this.addHeaderOffer(
      name,
      "static",
      "X-Fixture-Key",
      "fixture-static-secret",
    );
  }

  private async addHeaderOffer(
    name: string,
    source: "static" | "request",
    header: string,
    value: string,
  ) {
    const add = this.page.getByRole("button", {
      name: "Add credential",
      exact: true,
    });
    await expect(
      add,
      "UI_TYPED_CREDENTIAL_BLOCKER: Settings must expose Upstream credentials",
    ).toBeVisible();
    await add.click();
    const dialog = this.page.getByRole("dialog");
    const type = dialog.getByRole("radio", {
      name:
        source === "static" ? /Static credential/ : /Caller-supplied header/,
    });
    await expect(
      type,
      "UI_TYPED_CREDENTIAL_CAPABILITY_BLOCKER: requested header type must be available on this server; absence blocks fixture construction before runtime",
    ).toBeVisible();
    await type.focus();
    await type.press("Space");
    await dialog.getByLabel("Name", { exact: true }).fill(name);
    await dialog.getByLabel("Header name", { exact: true }).fill(header);
    await dialog
      .getByLabel(
        source === "static"
          ? `Value for ${header}`
          : `Caller header for ${header}`,
        { exact: true },
      )
      .fill(value);
    await dialog
      .getByRole("button", { name: "Add credential", exact: true })
      .click();
    await expect(dialog).not.toBeVisible();
    await expect(
      this.page.getByRole("button", { name: new RegExp(name) }).first(),
    ).toBeVisible();
  }

  async issueAgentKey(
    serverName: string,
    credentialName: string,
    upstreamURL: string,
  ) {
    await this.page.goto(`${this.projectURL}/identities/agents/new`);
    const name = `fixture-agent-${Date.now()}`;
    const field = this.page
      .locator(".gram-elements")
      .getByRole("textbox", { name: "Name", exact: true });
    await expect(field).toBeEditable();
    await field.fill(name);
    await expect(
      field,
      "UI_AGENT_NAME_BLOCKER: the wizard must retain the entered name",
    ).toHaveValue(name);
    await field.press("Tab");
    const next = this.page.getByRole("button", {
      name: "Continue",
      exact: true,
    });
    await expect(
      next,
      "UI_AGENT_NAME_BLOCKER: a fresh valid name must pass dashboard validation",
    ).toBeEnabled();
    await next.click();
    await expect(
      this.page.getByRole("heading", { name: "MCP Selection", exact: true }),
    ).toBeVisible();
    const upstream = new URL(upstreamURL);
    const visibleName = `${upstream.host}${upstream.pathname}`;
    const server = this.page
      .getByRole("checkbox", { name: serverName, exact: true })
      .or(this.page.getByRole("checkbox", { name: visibleName, exact: true }));
    await expect(
      server,
      "UI_AGENT_INVENTORY_BLOCKER: the new connection must appear once in the wizard",
    ).toHaveCount(1);
    const label = await server.getAttribute("aria-label");
    await server.check();
    const choice = this.page.getByRole("combobox", {
      name: `Credential the key uses on ${label}`,
      exact: true,
    });
    await expect(
      choice,
      "UI_AGENT_CHOICE_BLOCKER: agent wizard must expose an explicit credential choice for this multi-offer server",
    ).toBeVisible();
    await choice.click();
    const escaped = credentialName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const selected = this.page.getByRole("option", {
      name: new RegExp(`^${escaped} · `),
    });
    await expect(
      selected,
      "UI_AGENT_CHOICE_BLOCKER: select the exact named credential, not a prefix match",
    ).toHaveCount(1);
    await selected.click();
    await expect(choice).toContainText(credentialName);
    await this.page
      .getByRole("button", { name: "Continue", exact: true })
      .click();
    await expect(
      this.page.getByRole("heading", { name: "MCP sign-in", exact: true }),
    ).toBeVisible();
    const signInNext = this.page.getByRole("button", {
      name: "Continue",
      exact: true,
    });
    await expect(
      signInNext,
      "UI_AGENT_CONNECTION_BLOCKER: selected shared credential must allow progression",
    ).toBeEnabled();
    await signInNext.click();
    await expect(
      this.page.getByRole("heading", { name: "Agent sign-in", exact: true }),
    ).toBeVisible();
    const apiKey = this.page.getByRole("radio", { name: /^API key/ });
    await apiKey.focus();
    await apiKey.press("Space");
    await this.page
      .getByRole("button", { name: "Create agent", exact: true })
      .click();
    await this.page.getByRole("tab", { name: "Manual", exact: true }).click();
    const key = this.page.getByRole("group", {
      name: "API key — shown once",
      exact: true,
    });
    await expect(
      key,
      "UI_AGENT_ISSUANCE_BLOCKER: dashboard must show the freshly issued key",
    ).toBeVisible();
    const value = (await key.locator("code").innerText()).trim();
    if (!value || value.includes("<your"))
      throw new Error(
        "UI_AGENT_ISSUANCE_BLOCKER: dashboard returned a placeholder, not an issued key",
      );
    return value;
  }

  async finishAgentSetup() {
    await this.page
      .getByRole("button", { name: "Continue to verification", exact: true })
      .click();
    // Runtime protocol assertions follow in the scenario. Do not pretend the
    // wizard has observed a first gateway call before one has been made.
    const finish = this.page
      .getByRole("button", { name: "Done", exact: true })
      .or(this.page.getByRole("button", { name: "Skip for now", exact: true }));
    await expect(finish).toBeVisible();
    await finish.click();
    await expect(
      this.page.getByRole("link", { name: "Credentials", exact: true }),
    ).toBeVisible();
  }

  async organizationGateway() {
    await this.page.goto(`${this.site}/${this.organizationSlug}/gateway`);
    const toggle = this.page.getByRole("switch", {
      name: "Organization Gateway",
      exact: true,
    });
    await expect(toggle).toBeVisible();
    if (!(await toggle.isChecked())) {
      this.changedGateway = true;
      await toggle.click();
      await this.page
        .getByRole("dialog")
        .getByRole("button", { name: "Turn on", exact: true })
        .click();
      await expect(toggle).toBeChecked();
    }
    const heading = this.page.getByRole("heading", {
      name: "Endpoint URL",
      exact: true,
    });
    await expect(
      heading,
      "UI_GATEWAY_BLOCKER: enable Organization Gateway and configure its shared issuer through dashboard settings",
    ).toBeVisible();
    const endpoint = (
      await heading.locator("..").locator("code").innerText()
    ).trim();
    expect(endpoint).toMatch(/^https?:\/\//);
    return endpoint;
  }

  async enforce() {
    const admin = process.env.GRAM_ADMIN_URL;
    if (!admin)
      throw new Error(
        "UI_ENFORCEMENT_BLOCKER: set GRAM_ADMIN_URL to the local staff dashboard; /upstream-oauth-migration must show this organization ready and Enforce must be confirmed. Legacy/shadow results are not enforcement proof.",
      );
    const staff = await this.page.context().newPage();
    try {
      await staff.goto(`${admin}/upstream-oauth-migration`);
      const filter = staff.getByRole("textbox", {
        name: "Filter organizations",
      });
      const engineering = staff.getByRole("button", {
        name: /^Engineering User/,
      });
      await expect(
        filter.or(engineering),
        "UI_STAFF_LOGIN_BLOCKER: local staff mock-idp must offer Engineering User",
      ).toBeVisible();
      if (await engineering.isVisible()) await engineering.click();
      await expect(filter).toBeVisible();
      await filter.fill(this.organizationSlug);
      const row = staff
        .getByRole("row")
        .filter({ hasText: this.organizationSlug });
      await expect(row).toHaveCount(1);
      await expect(
        row.getByText(
          /Upstream auth enforced|Ready to enforce|Not ready to enforce/,
        ),
        "UI_ENFORCEMENT_READINESS_BLOCKER: staff readiness must finish before testing auth",
      ).toBeVisible({ timeout: 60_000 });
      const stopped = row.getByRole("button", {
        name: "Stop enforcing",
        exact: true,
      });
      if (await stopped.isVisible()) {
        await this.verifyPersistedEnforcement(staff);
        return;
      }
      const migrate = row.getByRole("button", {
        name: /^(Migrate|Run again)$/,
      });
      await expect(migrate).toBeEnabled();
      await migrate.click();
      await staff
        .getByRole("dialog")
        .getByRole("button", { name: "Migrate", exact: true })
        .click();
      await expect(migrate).toBeEnabled();
      await expect(
        row.getByText(
          /Upstream auth enforced|Ready to enforce|Not ready to enforce/,
        ),
        "UI_ENFORCEMENT_READINESS_BLOCKER: staff readiness must finish before testing auth",
      ).toBeVisible({ timeout: 60_000 });
      if (await stopped.isVisible()) {
        await this.verifyPersistedEnforcement(staff);
        return;
      }
      const enforce = row.getByRole("button", { name: "Enforce", exact: true });
      await expect(
        enforce,
        "UI_ENFORCEMENT_READINESS_BLOCKER: inspect migration readiness and blockers; no API or SQL workaround is allowed",
      ).toBeEnabled();
      await enforce.click();
      this.changedEnforcement = true;
      await staff
        .getByRole("dialog")
        .getByRole("button", { name: "Enforce", exact: true })
        .click();
      await expect(stopped).toBeVisible();
      await this.verifyPersistedEnforcement(staff);
    } finally {
      await staff.close();
    }
  }

  private async verifyPersistedEnforcement(staff: Page) {
    // Re-read persisted state after a fresh page load, not a mutation's cached UI.
    await staff.reload();
    const filter = staff.getByRole("textbox", { name: "Filter organizations" });
    await expect(filter).toBeVisible();
    await filter.fill(this.organizationSlug);
    const row = staff
      .getByRole("row")
      .filter({ hasText: this.organizationSlug });
    await expect(row).toHaveCount(1);
    await expect(
      row.getByText(/^Upstream auth enforced\./),
      "UI_ENFORCEMENT_BLOCKER: persisted typed-client enforcement must be enabled before every scenario",
    ).toBeVisible({ timeout: 60_000 });
    await expect(
      row.getByRole("button", { name: "Stop enforcing", exact: true }),
    ).toBeVisible();
  }

  async restoreEnforcement() {
    if (this.changedGateway) {
      await this.page.goto(`${this.site}/${this.organizationSlug}/gateway`);
      const toggle = this.page.getByRole("switch", {
        name: "Organization Gateway",
        exact: true,
      });
      await expect(toggle).toBeVisible();
      if (await toggle.isChecked()) {
        await toggle.click();
        await this.page
          .getByRole("dialog")
          .getByRole("button", { name: "Turn off", exact: true })
          .click();
        await expect(toggle).not.toBeChecked();
      }
      this.changedGateway = false;
    }
    if (!this.changedEnforcement) return;
    const admin = process.env.GRAM_ADMIN_URL;
    if (!admin)
      throw new Error(
        "UI_CLEANUP_BLOCKER: local staff dashboard is required to restore enforcement",
      );
    const staff = await this.page.context().newPage();
    try {
      await staff.goto(`${admin}/upstream-oauth-migration`);
      const filter = staff.getByRole("textbox", {
        name: "Filter organizations",
      });
      await expect(filter).toBeVisible();
      await filter.fill(this.organizationSlug);
      const row = staff
        .getByRole("row")
        .filter({ hasText: this.organizationSlug });
      await expect(row).toHaveCount(1);
      await expect(
        row.getByText(
          /Upstream auth enforced|Ready to enforce|Not ready to enforce/,
        ),
      ).toBeVisible();
      const stop = row.getByRole("button", {
        name: "Stop enforcing",
        exact: true,
      });
      if (await stop.isVisible()) {
        await stop.click();
        await staff
          .getByRole("dialog")
          .getByRole("button", { name: "Stop enforcing", exact: true })
          .click();
        await expect(stop).not.toBeVisible();
      }
      this.changedEnforcement = false;
    } finally {
      await staff.close();
    }
  }
}

/** Drives only real consent documents and the mock provider's human approval form. */
export async function approveAuthorization(
  page: Page,
  authorizationURL: string,
) {
  await page.goto(authorizationURL);
  for (let step = 0; step < 20; step++) {
    if (
      /\/(callback|oauth\/callback)(?:\?|$)/.test(page.url()) &&
      new URL(page.url()).hostname === "127.0.0.1"
    )
      return;
    const approve = page.getByRole("button", {
      name: "Approve mock access",
      exact: true,
    });
    const give = page.getByRole("button", { name: "Give access", exact: true });
    const login = page.getByRole("button", { name: "Log in", exact: true });
    const allTools = page.getByRole("button", {
      name: "Give access to all tools",
      exact: true,
    });
    const connect = page
      .getByRole("link", { name: /^(?:Connect|Reconnect)(?:\s|$)/ })
      .first();
    const connectButton = page
      .getByRole("button", { name: /^(?:Connect|Reconnect)(?:\s|$)/ })
      .first();
    const inventoryFailure = page.getByText(
      "Tools could not be captured. This gateway cannot be approved. Try again later.",
      { exact: true },
    );
    // A disabled approval button is not a transition. Wait for an actionable
    // control or report the actual consent failure instead of spinning on it.
    await expect
      .poll(
        async () =>
          (await inventoryFailure.isVisible()) ||
          (await allTools.isVisible()) ||
          (await approve.isVisible()) ||
          (await login.isVisible()) ||
          ((await give.isVisible()) && (await give.isEnabled())) ||
          (await connect.isVisible()) ||
          (await connectButton.isVisible()),
        {
          timeout: 10_000,
          message: "UI_CONSENT_BLOCKER: no actionable consent control",
        },
      )
      .toBe(true);
    if (await inventoryFailure.isVisible()) {
      throw new Error(
        "UI_CONSENT_INVENTORY_BLOCKER: tools could not be captured; organization gateway approval is disabled",
      );
    }
    if (await allTools.isVisible()) await allTools.click();
    else if (await approve.isVisible()) await approve.click();
    else if (await login.isVisible()) await login.click();
    else if ((await give.isVisible()) && (await give.isEnabled())) {
      await give.click();
      return;
    } else if (await connect.isVisible()) await connect.click();
    else if (await connectButton.isVisible()) await connectButton.click();
  }
  throw new Error(
    "UI_CONSENT_BLOCKER: consent never reached Give access after 20 attempted UI transitions",
  );
}
