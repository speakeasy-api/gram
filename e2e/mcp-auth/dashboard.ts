import { randomUUID } from "node:crypto";
import { expect, test, type Page, type Request } from "@playwright/test";
import { authorizationFailure } from "./authorization-diagnostics.ts";
import { addPersonalOffer } from "./personal-offer-actions.ts";
import {
  projectMode,
  recordSharedProject,
  sharedProject,
  type OwnedProject,
} from "./project-mode.ts";

/** All product provisioning is deliberately driven by visible dashboard controls. */
export class Dashboard {
  projectURL = "";
  private projectName = "";
  readonly createdProjects: OwnedProject[] = [];
  readonly sharedProjects: OwnedProject[] = [];
  organizationSlug = "";
  constructor(
    readonly page: Page,
    readonly site = process.env.GRAM_SITE_URL ?? "",
  ) {
    if (!site)
      throw new Error(
        "UI_PREREQUISITE: GRAM_SITE_URL must identify the local dashboard",
      );
  }

  get ownedProjects() {
    return [...this.createdProjects, ...this.sharedProjects];
  }

  async login() {
    const login = this.page.getByRole("button", {
      name: "Log in",
      exact: true,
    });
    const add = this.page.getByRole("button", { name: "Add New", exact: true });
    const home = this.page.getByRole("link", { name: "Home", exact: true });
    const uiOrigin = new URL(this.site).origin;
    let failedModules = 0;
    const observeModuleFailure = (request: Request) => {
      const url = new URL(request.url());
      if (
        url.origin === uiOrigin &&
        /\.(?:ts|tsx|js|jsx)$/.test(url.pathname) &&
        request.resourceType() === "script" &&
        request.failure()?.errorText === "net::ERR_FAILED"
      ) {
        failedModules++;
      }
    };
    this.page.on("requestfailed", observeModuleFailure);
    try {
      await this.page.goto(this.site);
      try {
        await expect(login.or(add).or(home).first()).toBeVisible();
      } catch (originalFailure) {
        // Only a timed-out initial render can recover, never a login/API failure.
        const matcher = (
          originalFailure as {
            matcherResult?: { name?: string; timeout?: number };
          }
        )?.matcherResult;
        if (
          matcher?.name !== "toBeVisible" ||
          typeof matcher.timeout !== "number" ||
          matcher.timeout <= 0 ||
          failedModules === 0
        ) {
          throw originalFailure;
        }
        const blankDevShell = await this.page
          .evaluate((origin) => {
            return (
              location.origin === origin &&
              document.body !== null &&
              document.body.innerText.trim() === "" &&
              document.querySelector('script[src="/@vite/client"]') !== null &&
              document.querySelector(
                '.gram-elements, button, [role="button"], a[href], input[type="button"], input[type="submit"], input[type="reset"]',
              ) === null
            );
          }, uiOrigin)
          .catch(() => false);
        if (!blankDevShell) throw originalFailure;
        test.info().annotations.push({
          type: "ui-bootstrap-module-recovery",
          description: JSON.stringify({
            attempted: true,
            reloadCount: 1,
            failedModuleCount: failedModules,
          }),
        });
        // One clean root load before login or any enforcement/provisioning.
        await this.page.goto(new URL("/", this.site).href);
        await expect(login.or(add).or(home).first()).toBeVisible();
      }
    } finally {
      this.page.off("requestfailed", observeModuleFailure);
    }
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

  /** Follow rendered SPA links so administration does not reload the dev shell. */
  async projectRoute(route: "mcp" | "remote-identity-providers") {
    const projectPath = new URL(this.projectURL).pathname;
    const location = new URL(this.page.url());
    if (
      location.origin !== new URL(this.projectURL).origin ||
      /^\/(?:oauth|mcp)\//.test(location.pathname)
    ) {
      // A completed upstream flow leaves the page on its protocol callback,
      // which has no dashboard sidebar. Return to the recorded UI project;
      // only mounted dashboard routes can use the SPA Home/navigation links.
      await this.page.goto(this.projectURL);
      await expect(this.page).toHaveURL(this.projectURL);
    }
    const back = this.page.getByRole("link", {
      name: /^Back to all (?:servers|MCPs|gateways)$/,
    });
    if (await back.isVisible()) await back.click();
    const current = new URL(this.page.url()).pathname;
    if (current !== projectPath && !current.startsWith(`${projectPath}/`)) {
      const owned = this.ownedProjects.find((p) => p.url === this.projectURL);
      if (!owned || !owned.name.startsWith("auth-e2e-"))
        throw new Error(
          "Refusing to navigate to a project not created by this fixture",
        );
      await this.page.getByRole("link", { name: "Home", exact: true }).click();
      await this.page
        .getByPlaceholder("Search projects...", { exact: true })
        .fill(owned.name);
      await this.page
        .getByRole("link", { name: `Open ${owned.name}`, exact: true })
        .click();
      await expect(this.page).toHaveURL(this.projectURL);
    }
    const target = `${projectPath}/${route}`;
    if (new URL(this.page.url()).pathname !== target) {
      const group = route === "mcp" ? "MCP Gateway" : "Identity";
      const expand = this.page.getByRole("button", {
        name: `Expand ${group}`,
        exact: true,
      });
      const collapse = this.page.getByRole("button", {
        name: `Collapse ${group}`,
        exact: true,
      });
      // The URL can change before the organization shell mounts the project
      // sidebar. Wait for its group controls before deciding whether to expand.
      await expect(expand.or(collapse)).toBeVisible();
      if (await expand.isVisible()) await expand.click();
      // The nested link is the route item, not the group header's toggle.
      const link = this.page.locator(
        `li[data-sidebar="menu-item"] ul a[href="${target}"]`,
      );
      await expect(link).toHaveCount(1);
      await link.click();
    }
    await this.page.waitForURL((url) => url.pathname === target);
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

  /** Teardown removes created projects only; the shared one outlives the test. */
  async enterProject() {
    const name = `auth-e2e-${randomUUID().slice(0, 8)}`;
    if (projectMode() === "isolated") {
      await this.createProject(name);
      return;
    }
    const existing = sharedProject();
    if (existing) {
      await this.page.goto(existing.url);
      this.projectName = existing.name;
      this.projectURL = existing.url;
      this.sharedProjects.push(existing);
      await expect(
        this.page.getByRole("button", {
          name: `${existing.name} Project`,
          exact: true,
        }),
      ).toBeVisible();
      return;
    }
    await this.createProject(name);
    const created = this.createdProjects.pop()!;
    this.sharedProjects.push(created);
    recordSharedProject(created);
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
    await this.projectRoute("mcp");
    // Header and empty-state buttons open the same add flow.
    await this.page
      .getByRole("button", { name: "Add new", exact: true })
      .first()
      .click();
    await this.page
      .getByRole("link")
      .filter({
        has: this.page.getByText("Hosted remotely", { exact: true }),
      })
      .click();
    await this.page.waitForURL(`${this.projectURL}/mcp/add/remote`);
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
      await input.fill(`fixture-${randomUUID().slice(0, 8)}`);
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

  async personalStaticHeader(
    registrationExternalId: string,
    registrationRowId: string,
  ) {
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
    const name = `fixture-personal-with-header-${randomUUID().slice(0, 8)}`;
    await addPersonalOffer(
      this.page,
      name,
      registrationExternalId,
      registrationRowId,
    );
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

  /** Run once by enforcement.setup.ts; it stays on for every later run. */
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
      const verifyPersisted = async () => {
        await staff.reload();
        await expect(filter).toBeVisible();
        await filter.fill(this.organizationSlug);
        await expect(row).toHaveCount(1);
        await expect(
          row.getByText("Upstream auth enforced", { exact: true }),
        ).toBeVisible({ timeout: 60_000 });
        await expect(stopped).toBeVisible();
      };
      const stopped = row.getByRole("button", {
        name: "Stop enforcing",
        exact: true,
      });
      if (await stopped.isVisible()) {
        await verifyPersisted();
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
        await verifyPersisted();
        return;
      }
      const enforce = row.getByRole("button", { name: "Enforce", exact: true });
      await expect(
        enforce,
        "UI_ENFORCEMENT_READINESS_BLOCKER: inspect migration readiness and blockers; no API or SQL workaround is allowed",
      ).toBeEnabled();
      await enforce.click();
      await staff
        .getByRole("dialog")
        .getByRole("button", { name: "Enforce", exact: true })
        .click();
      await expect(stopped).toBeVisible();
      await verifyPersisted();
    } finally {
      await staff.close();
    }
  }
}

/** Drives only real consent documents and the mock provider's human approval form. */
export async function approveAuthorization(
  page: Page,
  authorizationURL: string,
  observe?: () => Promise<void>,
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
    const configurationChanged = page.getByText(
      "This server’s configuration changed. Reload this page to review access.",
      { exact: true },
    );
    // A disabled approval button is not a transition. Wait for an actionable
    // control or report the actual consent failure instead of spinning on it.
    try {
      await expect
        .poll(
          async () =>
            (await page.locator("details[data-error-code]").isVisible()) ||
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
    } catch (originalFailure) {
      // A stale inventory can coexist with a valid Connect action. Classify it
      // only when no supported action appeared; never reload or retry consent.
      if (await configurationChanged.isVisible()) {
        await observe?.();
        throw new Error(
          "UI_CONSENT_CONFIGURATION_CHANGED: inventory requires reload; approval remains disabled",
        );
      }
      throw originalFailure;
    }
    await observe?.();
    const failure = await authorizationFailure(page);
    if (failure)
      throw new Error(`AUTHORIZATION_REFUSED: ${JSON.stringify(failure)}`);
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
