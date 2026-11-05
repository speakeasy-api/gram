import { mkdir, open } from "node:fs/promises";
import { dirname } from "node:path";
import {
  test,
  type Locator,
  type Page,
  type Request,
  type Response,
  type ConsoleMessage,
  type Frame,
} from "@playwright/test";
import { expect, type AuthScenario } from "./fixtures.ts";
import { authorizationFailure } from "./authorization-diagnostics.ts";

type AudienceKind = "Agent" | "Person" | "Everyone";
type AudienceCheckpoint =
  | "last"
  | "after-add-draft"
  | "after-save"
  | "credential-open";

type PermissionStage =
  | "relationship"
  | "account"
  | "owner"
  | "open-credential"
  | "grant-agent"
  | "grant-person"
  | "save"
  | "reload-verify"
  | "profile";

/** Real managed-assistant turns, not the client-side onboarding preview. */
export class AssistantAuthActions {
  private settingsURL = "";
  private submissionErrors: string[] = [];
  private submissionObservation: Record<string, unknown> = {};
  private permissionStage: PermissionStage = "relationship";
  private permissionPhaseCount = 0;
  private audienceEvidence: Partial<
    Record<AudienceCheckpoint, Record<string, unknown>>
  > = {};

  private permissionPhase(stage: PermissionStage) {
    this.permissionStage = stage;
    this.permissionPhaseCount += 1;
  }
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
    const { page, dashboard } = this.auth;
    try {
      // Use the rendered SPA route back to the same recorded assistant. Preserve
      // the earlier document reload checks of each credential's persisted grants.
      const credentialSheet = page.getByRole("dialog").filter({
        has: page.getByRole("heading", {
          name: "Who can use this",
          exact: true,
        }),
      });
      if (await credentialSheet.count()) {
        await expect(credentialSheet).toHaveCount(1);
        await credentialSheet
          .getByRole("button", { name: "Close", exact: true })
          .click();
        await expect(credentialSheet).toHaveCount(0);
      }
      const back = page.getByRole("link", {
        name: "Back to all servers",
        exact: true,
      });
      if (await back.isVisible()) await back.click();
      const expand = page.getByRole("button", {
        name: "Expand MCP Gateway",
        exact: true,
      });
      const collapse = page.getByRole("button", {
        name: "Collapse MCP Gateway",
        exact: true,
      });
      // A route transition must render its project navigation before we decide
      // whether expansion is needed. Scope-gated absence remains a failure.
      const gatewayToggle = expand.or(collapse);
      await expect(gatewayToggle).toHaveCount(1);
      await expect(gatewayToggle).toBeVisible();
      if (await expand.isVisible()) await expand.click();
      await expect(collapse).toBeVisible();
      const list = page.getByRole("link", {
        name: "Assistants Beta",
        exact: true,
      });
      await expect(list).toHaveCount(1);
      const listHref = await list.getAttribute("href");
      expect(
        listHref !== null &&
          new URL(listHref, page.url()).href ===
            new URL(`${dashboard.projectURL}/assistants`).href,
        "SETUP: assistant list belongs to the same project",
      ).toBe(true);
      await list.click();
      const target = new URL(this.settingsURL);
      const detail = page
        .getByRole("link")
        .and(
          page.locator(
            `a[href=${JSON.stringify(target.pathname + target.search + target.hash)}]`,
          ),
        );
      await expect(detail).toHaveCount(1);
      await detail.click();
      await expect(page).toHaveURL(this.settingsURL);
      await this.identity();
    } catch (error) {
      const written = await this.captureTerminalPage(
        this.auth.page,
        "profile",
        this.settingsURL,
        error,
      );
      throw new Error(
        `SETUP_ASSISTANT_TERMINAL: stage=profile privateCaptureWritten=${written}`,
      );
    }
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

  /** Client selection is not a grant: authorize only the exercised clients. */
  async grantCredentials(
    serverURL: string,
    serverName: string,
    clients: string[],
  ) {
    this.permissionPhaseCount = 0;
    this.audienceEvidence = {};
    this.permissionPhase("relationship");
    try {
      await this.configureCredentialAudience(serverURL, serverName, clients);
    } catch (error) {
      // Raw locator errors can name people or contain identity URLs. This file
      // is private output, never an attachment or part of the public error.
      let privateCaptureWritten = false;
      try {
        const path = test
          .info()
          .outputPath("private-assistant-audience-error.json");
        await mkdir(dirname(path), { recursive: true, mode: 0o700 });
        const file = await open(path, "wx", 0o600);
        try {
          await file.writeFile(
            JSON.stringify(
              {
                phase: this.permissionStage,
                phaseCount: this.permissionPhaseCount,
                audienceEvidence: this.audienceEvidence,
                message: error instanceof Error ? error.message : String(error),
                stack: error instanceof Error ? error.stack : null,
              },
              null,
              2,
            ),
          );
          privateCaptureWritten = true;
        } finally {
          await file.close();
        }
      } catch {
        // Capture failure must not disclose either the path or raw error.
        privateCaptureWritten = false;
      }
      throw new Error(
        `SETUP_ASSISTANT_AUDIENCE: stage=${this.permissionStage} privateCaptureWritten=${privateCaptureWritten}`,
      );
    }
  }

  private async configureCredentialAudience(
    serverURL: string,
    serverName: string,
    clients: string[],
  ) {
    const { page } = this.auth;
    await this.identity();
    const profileLabel = await this.profile(serverName).innerText();
    // ConfiguredIdentity links the assistant's actual agent, not its owner.
    const agentLink = page.locator('a[href*="/identities/agent%3A"]');
    await expect(agentLink).toHaveCount(1);
    const agentHref = await agentLink.getAttribute("href");
    await expect
      .poll(
        async () => {
          const name = (await agentLink.innerText()).trim();
          return name !== "" && name !== "Agent identity";
        },
        { message: "SETUP: configured agent name has resolved" },
      )
      .toBe(true);
    const agentName = (await agentLink.innerText()).trim();
    expect(
      Boolean(agentHref && agentName && agentName !== "Agent identity"),
    ).toBe(true);

    this.permissionPhase("account");
    await page
      .getByRole("button", { name: "Account menu", exact: true })
      .click();
    const account = page.getByRole("menu").locator('a[href*="/identities/"]');
    await expect(account).toHaveCount(1);
    const personHref = await account.getAttribute("href");
    const personName = (await account.locator("p").nth(0).innerText()).trim();
    const personEmail = (await account.locator("p").nth(1).innerText()).trim();
    expect(Boolean(personHref && personName && personEmail)).toBe(true);
    await page.keyboard.press("Escape");

    this.permissionPhase("owner");
    await agentLink.click();
    const owner = page
      .locator("dl > div")
      .filter({
        has: page.locator("dt").filter({ hasText: /^Owner$/ }),
      })
      .locator("dd a");
    await expect(owner).toHaveCount(1);
    const ownerHref = await owner.getAttribute("href");
    await expect
      .poll(
        async () => {
          const name = (
            await owner.locator("span.truncate").innerText()
          ).trim();
          return name !== "" && name !== "Unavailable";
        },
        { message: "SETUP: linked agent owner name has resolved" },
      )
      .toBe(true);
    const ownerName = (await owner.locator("span.truncate").innerText()).trim();
    expect(Boolean(ownerHref && ownerName && ownerName !== "Unavailable")).toBe(
      true,
    );
    // Compare principal paths, ignoring the identity activity-window query.
    const sameOwner =
      new URL(ownerHref!, page.url()).pathname ===
      new URL(personHref!, page.url()).pathname;
    const people = [{ name: personName, email: personEmail }];
    if (!sameOwner) people.push({ name: ownerName, email: "" });

    for (const client of clients) {
      // Reopening after reload must not start a second document navigation.
      const open = async () => {
        const button = page.getByRole("button").filter({
          has: page.getByText(client, { exact: true }),
        });
        try {
          await expect
            .poll(() => button.count(), {
              message: "SETUP: unique exercised credential",
            })
            .toBe(1);
        } catch (error) {
          // Diagnostics only: preserve the failed lookup and never change policy.
          // The outer catch writes this evidence privately with mode 0600.
          this.audienceEvidence["credential-open"] = {
            phase: "credentialopen",
            captureComplete: false,
          };
          try {
            const actual = new URL(page.url());
            const expected = new URL(serverURL);
            const project = new URL(this.auth.dashboard.projectURL);
            const includingHidden = page
              .getByRole("button", { includeHidden: true })
              .filter({
                has: page.getByText(client, { exact: true }),
              });
            const root = page.locator("#root");
            const rootCount = await root.count();
            const rootText =
              rootCount === 1 ? await root.innerText({ timeout: 1_000 }) : null;
            this.audienceEvidence["credential-open"] = {
              phase: "credentialopen",
              captureComplete: true,
              measurement: "sequential-non-atomic-not-policy-proof",
              siteOriginMatches: actual.origin === expected.origin,
              projectOriginMatches: actual.origin === project.origin,
              projectPathMatches: actual.pathname.startsWith(
                project.pathname.replace(/\/$/, "") + "/",
              ),
              pathnameEndsSettings: actual.pathname.endsWith("/settings"),
              rpcPath: /(?:^|\/)(?:rpc|rpc2)(?:\/|$)/.test(actual.pathname),
              callbackPath: /(?:^|\/)[^/]*callback[^/]*(?:\/|$)/i.test(
                actual.pathname,
              ),
              upstreamCredentialsCount: await page
                .locator("#upstream-credentials")
                .count(),
              visibleCredentialButtonCount: await includingHidden
                .filter({ visible: true })
                .count(),
              accessibleCredentialButtonCount: await button.count(),
              includeHiddenCredentialButtonCount: await includingHidden.count(),
              openDialogCount: await page.getByRole("dialog").count(),
              projectNavigationReady: await page
                .getByRole("link", {
                  name: "Project Assistant Beta",
                  exact: true,
                })
                .isVisible(),
              rootCount,
              rootTextEmpty: rootText === null ? null : rootText.trim() === "",
            };
            const snapshot = await page
              .locator("body")
              .ariaSnapshot({ timeout: 2_000 });
            this.audienceEvidence["credential-open"] = {
              ...this.audienceEvidence["credential-open"],
              ariaSnapshot: snapshot.slice(0, 16_384),
              snapshotCaptured: true,
            };
          } catch {
            this.audienceEvidence["credential-open"] = {
              ...this.audienceEvidence["credential-open"],
              diagnosticReadFailed: true,
            };
          }
          throw error;
        }
        await button.click();
        return page
          .getByRole("dialog")
          .filter({
            has: page.getByRole("heading", { name: client, exact: true }),
          })
          .locator("section")
          .filter({
            has: page.getByRole("heading", {
              name: "Who can use this",
              exact: true,
            }),
          });
      };
      this.permissionPhase("open-credential");
      await page.goto(`${serverURL}/settings`);
      let audience = await open();
      await expect(audience).toBeVisible();
      await this.ensureCredentialUse(audience, agentName, "Agent");
      for (const person of people) {
        await this.ensureCredentialUse(
          audience,
          person.name,
          "Person",
          person.email,
        );
      }
      // A fresh document and reopened sheet must show persisted grants.
      this.permissionPhase("reload-verify");
      await page.reload();
      audience = await open();
      await expect(audience).toBeVisible();
      await this.assertCredentialUse(audience, agentName, "Agent");
      for (const person of people) {
        await this.assertCredentialUse(audience, person.name, "Person");
      }
    }
    this.permissionPhase("profile");
    await this.settings();
    expect(
      (await agentLink.getAttribute("href")) === agentHref,
      "SETUP: assistant retains the same agent relationship",
    ).toBe(true);
    expect(
      (await this.profile(serverName).innerText()) === profileLabel,
      "SETUP: permission setup does not change profile selection",
    ).toBe(true);
  }

  private audienceRows(audience: Locator, name: string, kind: string) {
    return audience
      .getByRole("listitem")
      .filter({
        has: this.auth.page.getByText(name, { exact: true }),
      })
      .filter({ has: this.auth.page.getByText(kind, { exact: true }) });
  }

  private async captureAudienceState(
    audience: Locator,
    name: string,
    kind: AudienceKind,
    checkpoint: AudienceCheckpoint,
  ) {
    const rows = this.audienceRows(audience, name, kind);
    const groups = rows.getByRole("group", {
      name: `Access for ${name}`,
      exact: true,
    });
    const inherited = rows.filter({
      hasNot: this.auth.page.getByRole("group"),
    });
    // SegmentedControl uses CSS uppercase and renders BOTH options. Authority
    // comes from aria-pressed, never from the presence of the word Blocked.
    const use = groups.getByRole("button", { name: /^Can use$/i });
    const blocked = groups.getByRole("button", { name: /^Blocked$/i });
    const pressed = async (buttons: Locator) => {
      const values: string[] = [];
      for (const button of (await buttons.all()).slice(0, 32)) {
        const value = await button
          .getAttribute("aria-pressed", { timeout: 500 })
          .catch(() => null);
        values.push(
          value === "true"
            ? "true"
            : value === "false"
              ? "false"
              : "unavailable",
        );
      }
      return values;
    };
    const state = {
      measurement: "sequential-non-atomic-not-policy-proof",
      intendedKind: kind,
      phase: this.permissionStage,
      phaseCount: this.permissionPhaseCount,
      matchingRowsCount: await rows.count(),
      directGroupCount: await groups.count(),
      allGroupCount: await rows.getByRole("group").count(),
      directUseButtonCount: await use.count(),
      directUseExactCaseCount: await groups
        .getByRole("button", { name: "Can use", exact: true })
        .count(),
      directUsePressed: await pressed(use),
      directBlockedButtonCount: await blocked.count(),
      directBlockedPressed: await pressed(blocked),
      inheritedBlockedCount: await inherited.getByText(/^Blocked$/i).count(),
      inheritedUseCount: await inherited.getByText(/^Can use$/i).count(),
    };
    this.audienceEvidence[checkpoint] = state;
    // Snapshot capture must not replace the original policy failure. Bound the
    // stored content and wait; raw snapshots stay in the private 0600 capture.
    try {
      this.audienceEvidence[checkpoint] = {
        ...state,
        ariaSnapshot: (await audience.ariaSnapshot({ timeout: 2_000 })).slice(
          0,
          16_384,
        ),
        snapshotCaptured: true,
      };
    } catch {
      this.audienceEvidence[checkpoint] = { ...state, snapshotCaptured: false };
    }
    return state;
  }

  private async hasCredentialUse(
    audience: Locator,
    name: string,
    kind: AudienceKind,
  ): Promise<boolean> {
    // Wait for the editor, not a diagnostic count sampled during a rerender.
    await expect(audience).toBeVisible();
    await expect(
      audience.getByRole("button", { name: "Add", exact: true }),
    ).toBeEnabled();
    await expect(
      audience.getByRole("button", { name: "Save", exact: true }),
    ).toHaveCount(0);
    await expect(
      audience
        .locator("..")
        .getByText("Loading who can use this credential…", { exact: true }),
    ).toHaveCount(0);
    await this.captureAudienceState(audience, name, kind, "last");
    const rows = this.audienceRows(audience, name, kind);
    const groups = rows.getByRole("group", {
      name: `Access for ${name}`,
      exact: true,
    });
    const inherited = rows.filter({
      hasNot: this.auth.page.getByRole("group"),
    });
    // An actual block is never edited away, including read-only inherited rules.
    await expect(
      inherited.getByText(/^Blocked$/i),
      "SETUP: inherited block must not be overridden",
    ).toHaveCount(0);
    await expect(
      rows.getByRole("button", { name: /^Blocked$/i, pressed: true }),
      "SETUP: selected block must not be overridden",
    ).toHaveCount(0);
    if (await rows.getByRole("group").count()) {
      await expect(
        groups,
        "SETUP: exact unambiguous principal control",
      ).toHaveCount(1);
      await expect(rows.getByRole("group")).toHaveCount(1);
      const use = groups.getByRole("button", { name: /^Can use$/i });
      const blocked = groups.getByRole("button", { name: /^Blocked$/i });
      await expect(use).toHaveCount(1);
      await expect(blocked).toHaveCount(1);
      await expect(
        blocked,
        "SETUP: block control must remain inactive",
      ).toHaveAttribute("aria-pressed", "false");
      await expect(use, "SETUP: use control must be active").toHaveAttribute(
        "aria-pressed",
        "true",
      );
      return true;
    }
    if (await inherited.getByText(/^Can use$/i).count()) {
      await expect(inherited.getByText(/^Can use$/i)).toBeVisible();
      return true;
    }
    // A present but unresolved row is not permission to add a new principal.
    await expect(
      rows,
      "SETUP: principal row must resolve before adding access",
    ).toHaveCount(0);
    // The personal user default does not give the agent permission.
    if (kind !== "Person") return false;
    return this.hasCredentialUse(
      audience,
      "Everyone in the organization",
      "Everyone",
    );
  }

  private async assertCredentialUse(
    audience: Locator,
    name: string,
    kind: AudienceKind,
  ) {
    await expect
      .poll(() => this.hasCredentialUse(audience, name, kind), {
        message:
          "SETUP: exercised credential has persisted principal use permission",
      })
      .toBe(true);
  }

  private async ensureCredentialUse(
    audience: Locator,
    name: string,
    kind: "Agent" | "Person",
    email = "",
  ) {
    this.permissionPhase(kind === "Agent" ? "grant-agent" : "grant-person");
    if (await this.hasCredentialUse(audience, name, kind)) return;
    await expect(
      audience.getByRole("button", { name: "Save", exact: true }),
    ).toHaveCount(0);
    await expect(
      audience
        .locator("..")
        .getByText("Loading who can use this credential…", { exact: true }),
    ).toHaveCount(0);
    await audience.getByRole("button", { name: "Add", exact: true }).click();
    const picker = this.auth.page.getByRole("dialog", {
      name: "Who else can use this credential?",
      exact: true,
    });
    await picker.getByRole("combobox", { name: /^Multi-select:/ }).click();
    const options = this.auth.page.getByRole("listbox", {
      name: "Available options",
    });
    // Kind is explicit in the agent description and the People option group.
    // Check uniqueness before narrowing by email, so duplicate names fail closed.
    const option =
      kind === "Agent"
        ? options.getByRole("option", {
            name: `${name}, not selected. Agent`,
            exact: true,
          })
        : options
            .getByRole("group", { name: "People", exact: true })
            .getByRole("option")
            .filter({ has: this.auth.page.getByText(name, { exact: true }) });
    expect(
      await option.count(),
      "SETUP: principal picker must be unique, never first-match",
    ).toBe(1);
    expect(
      (await option.getAttribute("aria-disabled")) !== "true",
      "SETUP: existing blocks must not be overridden",
    ).toBe(true);
    if (email) {
      expect(
        await option.getByText(email, { exact: true }).count(),
        "SETUP: selected person matches the signed-in account",
      ).toBe(1);
    }
    await option.click();
    await this.auth.page.keyboard.press("Escape");
    await picker.getByRole("button", { name: "Add", exact: true }).click();
    await this.captureAudienceState(audience, name, kind, "after-add-draft");
    this.permissionPhase("save");
    await audience.getByRole("button", { name: "Save", exact: true }).click();
    await expect(
      audience.getByRole("button", { name: "Save", exact: true }),
    ).toHaveCount(0);
    await this.captureAudienceState(audience, name, kind, "after-save");
    await this.assertCredentialUse(audience, name, kind);
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
    const prompt = `Call the fixture_identity tool on MCP server ${serverSlug} now. Do not infer or invent an identity. Return the actual tool result. If authentication is required, show its authorization link.`;
    this.submissionErrors = [];
    const onError = (error: Error) => {
      if (this.submissionErrors.length < 8)
        this.submissionErrors.push(error.stack ?? error.message);
    };
    const started = performance.now();
    const events: Record<string, unknown>[] = [];
    const privateUnknownErrors: string[] = [];
    this.submissionObservation = { events, privateUnknownErrors };
    const mark = (event: Record<string, unknown>) => {
      if (events.length < 240)
        events.push({ ms: Math.round(performance.now() - started), ...event });
    };
    const origin = new URL(page.url()).origin;
    const backend = process.env.GRAM_SERVER_URL
      ? new URL(process.env.GRAM_SERVER_URL).origin
      : origin;
    const paths: Record<string, string> = {
      "/rpc/assistants.sendMessage": "assistant-send",
      "/rpc/chat.load": "chat-load",
      "/rpc/chat.list": "chat-list",
      "/chat/turnstream": "turn-stream",
      "/chat/completions": "inference",
    };
    const classifyRequest = (request: Request) => {
      const url = new URL(request.url());
      if (
        !(url.pathname in paths) ||
        (url.origin !== origin && url.origin !== backend)
      )
        return null;
      return {
        operation: paths[url.pathname],
        sameOrigin: url.origin === origin,
        method: request.method(),
      };
    };
    const category = (text: string) => {
      if (text.includes("No user message to send."))
        return "empty-user-message";
      if (text.includes("Backend did not mint a chat id in time"))
        return "chat-id-not-minted";
      if (text.includes("Error creating stream:")) return "stream-creation";
      if (text.includes("Stream error in onError callback:"))
        return "stream-callback";
      if (text.includes("[turnstream] fell back to polling:"))
        return "stream-poll-fallback";
      if (text.includes("Failed to send queued assistant prompt:"))
        return "queued-prompt";
      if (
        text.includes("Error listing chats:") ||
        text.includes("Failed to list chats:")
      )
        return "chat-list";
      if (
        text.includes("Failed to load chat") ||
        text.includes("Error loading chat:")
      )
        return "chat-load";
      return "unclassified";
    };
    const onRequest = (request: Request) => {
      const match = classifyRequest(request);
      if (match) mark({ event: "request", ...match });
    };
    const onResponse = (response: Response) => {
      const match = classifyRequest(response.request());
      if (match)
        mark({ event: "response", ...match, status: response.status() });
    };
    const onFailure = (request: Request) => {
      const match = classifyRequest(request);
      if (match) {
        const failure = request.failure()?.errorText ?? "";
        mark({
          event: "request-failed",
          ...match,
          aborted: failure.includes("ERR_ABORTED"),
          connectionFailure: /ERR_CONNECTION|ERR_CERT|ERR_NAME/.test(failure),
        });
        if (privateUnknownErrors.length < 12)
          privateUnknownErrors.push(failure.slice(0, 2048));
      }
    };
    const pending: Promise<void>[] = [];
    const onFinished = (request: Request) => {
      const match = classifyRequest(request);
      if (!match) return;
      mark({ event: "request-finished", ...match });
      if (pending.length >= 12 || match.operation === "turn-stream") return;
      // Only already-finished bounded JSON errors; never read an SSE body.
      pending.push(
        (async () => {
          const response = await request.response();
          if (!response || response.status() < 400) return;
          const type = await response.headerValue("content-type");
          const length = Number(await response.headerValue("content-length"));
          if (!type?.includes("json") || !length || length > 8192) {
            mark({ event: "error-body-not-read", ...match });
            return;
          }
          const raw = await Promise.race([
            response.text(),
            new Promise<null>((resolve) =>
              setTimeout(() => resolve(null), 1000),
            ),
          ]);
          if (raw === null || raw.length > 8192) return;
          const body = JSON.parse(raw) as {
            code?: unknown;
            message?: unknown;
            name?: unknown;
          };
          const codes = [
            "bad_request",
            "unauthorized",
            "forbidden",
            "invalid_request",
            "not_found",
            "unexpected",
            "unavailable",
            "server_error",
          ];
          const code =
            typeof body.code === "string" && codes.includes(body.code)
              ? body.code
              : "unclassified";
          mark({
            event: "finite-error",
            ...match,
            code,
            category: category(raw),
          });
          if (privateUnknownErrors.length < 12)
            privateUnknownErrors.push(raw.slice(0, 2048));
        })().catch(() => {
          mark({ event: "error-body-read-failed", ...match });
        }),
      );
    };
    const onConsole = (message: ConsoleMessage) => {
      if (message.type() !== "error" && message.type() !== "warning") return;
      const text = message.text();
      mark({
        event: "console",
        level: message.type(),
        category: category(text),
      });
      if (privateUnknownErrors.length < 12)
        privateUnknownErrors.push(text.slice(0, 2048));
    };
    const onRoute = (frame: Frame) => {
      if (frame !== page.mainFrame()) return;
      const url = new URL(frame.url());
      mark({
        event: "route",
        sameOrigin: url.origin === origin,
        chatHome: /\/chat$/.test(url.pathname),
        conversation: /\/chat\/[^/]+$/.test(url.pathname),
      });
    };
    let sampling = false;
    let samples = 0;
    let activeSample: Promise<void> = Promise.resolve();
    const sample = async () => {
      if (sampling || samples >= 100) return;
      sampling = true;
      samples += 1;
      try {
        const notices = page
          .getByRole("alert")
          .or(page.getByRole("status"))
          .or(page.locator("[data-sonner-toast]"))
          .filter({ visible: true });
        const count = await notices.count();
        const input = page
          .getByRole("textbox", { name: "Message input", exact: true })
          .filter({ visible: true });
        const form = page
          .locator("form.aui-composer-root")
          .filter({ has: input });
        const url = new URL(page.url());
        mark({
          event: "rendered-state",
          noticeCount: count,
          chatHome: /\/chat$/.test(url.pathname),
          conversation: /\/chat\/[^/]+$/.test(url.pathname),
          composerEmpty: await form
            .getAttribute("data-empty", { timeout: 300 })
            .catch(() => null),
          sendEnabled: await page
            .getByRole("button", { name: "Send message", exact: true })
            .filter({ visible: true })
            .isEnabled({ timeout: 300 })
            .catch(() => false),
          modelControlCount: await page
            .getByRole("combobox", { name: /model/i })
            .count(),
          needsAdmin: await page
            .getByText(
              "Ask an admin to enable the Project Assistant for this project.",
              { exact: true },
            )
            .isVisible(),
        });
        for (const notice of (await notices.all()).slice(0, 4)) {
          const text = await notice.innerText({ timeout: 300 });
          mark({ event: "notice", category: category(text) });
          if (
            privateUnknownErrors.length < 12 &&
            !privateUnknownErrors.includes(text)
          )
            privateUnknownErrors.push(text.slice(0, 2048));
        }
      } catch {
        mark({ event: "rendered-read-incomplete" });
      } finally {
        sampling = false;
      }
    };
    page.on("pageerror", onError);
    page.on("request", onRequest);
    page.on("response", onResponse);
    page.on("requestfailed", onFailure);
    page.on("requestfinished", onFinished);
    page.on("console", onConsole);
    page.on("framenavigated", onRoute);
    const timer = setInterval(() => {
      if (!sampling) activeSample = sample();
    }, 250);
    try {
      const input = page
        .getByRole("textbox", { name: "Message input", exact: true })
        .filter({ visible: true });
      const composer = page
        .locator("form.aui-composer-root")
        .filter({ has: input });
      await input.fill(prompt);
      // The contenteditable's DOM changes before the shared composer state.
      // Wait for the rendered runtime-backed form to acknowledge this draft.
      await expect(input).toHaveText(prompt);
      // Nonempty composers omit data-empty; they do not render "false".
      await expect(composer).not.toHaveAttribute("data-empty", "true");
      await expect(input).toHaveText(prompt);
      const send = page
        .getByRole("button", { name: "Send message", exact: true })
        .filter({ visible: true });
      await expect(send).toBeEnabled();
      mark({ event: "verified-single-send-click" });
      await send.click();
      await expect(
        page,
        "SETUP: an actual submitted turn opens a conversation",
      ).toHaveURL(/\/chat\/[^/?#]+(?:[?#].*)?$/);
      await expect(
        page.getByText(prompt, { exact: true }).filter({ visible: true }),
        "SETUP: submitted prompt is rendered in the conversation",
      ).toBeVisible();
    } catch (error) {
      const written = await this.captureTerminalPage(
        page,
        "execution",
        this.auth.dashboard.projectURL,
        error,
      );
      throw new Error(
        `ASSISTANT_SUBMISSION_BLOCKER: privateCaptureWritten=${written}`,
      );
    } finally {
      clearInterval(timer);
      page.off("pageerror", onError);
      page.off("request", onRequest);
      page.off("response", onResponse);
      page.off("requestfailed", onFailure);
      page.off("requestfinished", onFinished);
      page.off("console", onConsole);
      page.off("framenavigated", onRoute);
      await activeSample;
      await Promise.all(pending);
      // Always preserve the passive boundary, even when the send succeeds.
      try {
        const path = test
          .info()
          .outputPath("private-assistant-submission-observation.json");
        await mkdir(dirname(path), { recursive: true, mode: 0o700 });
        const file = await open(path, "wx", 0o600);
        try {
          await file.writeFile(
            JSON.stringify(this.submissionObservation, null, 2),
          );
        } finally {
          await file.close();
        }
      } catch {
        /* Never replace the original send outcome with capture failure. */
      }
    }
  }

  async executedSince(offset: number, resource: string, principal: string) {
    try {
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
    } catch (error) {
      const written = await this.captureTerminalPage(
        this.auth.page,
        "execution",
        this.auth.dashboard.projectURL,
        error,
      );
      throw new Error(
        `ASSISTANT_EXECUTION_BLOCKER: stage=execution privateCaptureWritten=${written}`,
      );
    }
  }

  async authorize(serverSlug: string) {
    const { page } = this.auth;
    const link = page
      .locator(
        'a[href*="assistantMcpAuth"][href*="code_challenge"], a[href*="/mcp/connection_recovery/"]',
      )
      .filter({ visible: true });
    await expect(
      link,
      "ASSISTANT_OAUTH_BLOCKER: runtime must render its real authorization link",
    ).toBeVisible({ timeout: 90_000 });
    const href = await link.getAttribute("href");
    expect(href).toBeTruthy();
    const target = new URL(href!, page.url());
    const recovery = target.pathname.startsWith("/mcp/connection_recovery/");
    expect(
      target.origin === new URL(page.url()).origin,
      "ASSISTANT_OAUTH_BLOCKER: connection link must use the dashboard origin",
    ).toBe(true);
    const consent = await page.context().newPage();
    try {
      await consent.goto(target.href);
      await this.completeConsent(consent, recovery);
    } catch (error) {
      const written = await this.captureTerminalPage(
        consent,
        "consent",
        target.href,
        error,
      );
      throw new Error(
        `SETUP_ASSISTANT_TERMINAL: stage=consent privateCaptureWritten=${written}`,
      );
    } finally {
      // Cleanup must not replace the original failure captured above.
      await consent.close().catch(() => {});
    }
    // Connection recovery binds the represented person's account. Its completion
    // page asks for a retry; unlike assistantMcpAuth it does not resume the turn.
    if (recovery) await this.send(serverSlug);
  }

  private async captureTerminalPage(
    page: Page,
    stage: "consent" | "profile" | "execution",
    expectedURL: string,
    error: unknown,
  ): Promise<boolean> {
    const evidence: Record<string, unknown> = {
      stage,
      submissionErrors: this.submissionErrors,
      submissionObservation: this.submissionObservation,
      message: error instanceof Error ? error.message : String(error),
      stack: error instanceof Error ? error.stack : null,
    };
    // Each read is best effort. Diagnostics must not mask the original failure.
    const read = async (key: string, value: () => Promise<unknown>) => {
      try {
        evidence[key] = await value();
      } catch {
        evidence[key] = null;
      }
    };
    const control = async (key: string, locator: Locator) => {
      await read(key, async () => {
        const visible = locator.filter({ visible: true });
        const count = await visible.count();
        return {
          count,
          visible: count > 0,
          enabled:
            count === 1 ? await visible.isEnabled({ timeout: 500 }) : null,
        };
      });
    };
    await read("urlClassification", async () => {
      const expected = new URL(expectedURL);
      const actual = new URL(page.url());
      return {
        originMatchesExpected: actual.origin === expected.origin,
        pathnameMatchesExpected: actual.pathname === expected.pathname,
        targetPathRecovery: expected.pathname.startsWith(
          "/mcp/connection_recovery/",
        ),
        currentPathRecovery: actual.pathname.startsWith(
          "/mcp/connection_recovery/",
        ),
      };
    });
    await read("officialErrorDetailsPresent", () =>
      page.locator("details[data-error-code]").isVisible(),
    );
    await read("officialError", async () => {
      const failure = await authorizationFailure(page);
      return failure === null
        ? null
        : { code: failure.code, reason: failure.reason };
    });
    if (stage === "consent") {
      await read("doneHeadingVisible", () =>
        page
          .getByRole("heading", { name: "Account connected", exact: true })
          .isVisible(),
      );
      await read("legacyDoneVisible", () =>
        page
          .getByText("Authentication complete. You can close this window.", {
            exact: true,
          })
          .isVisible(),
      );
      await control(
        "login",
        page.getByRole("button", { name: "Log in", exact: true }),
      );
      await control(
        "approve",
        page.getByRole("button", { name: "Approve mock access", exact: true }),
      );
      await control(
        "giveAccess",
        page.getByRole("button", { name: "Give access", exact: true }),
      );
      await control(
        "connectButton",
        page.getByRole("button", { name: /^(Connect|Reconnect)(\s|$)/ }),
      );
      await control(
        "connectLink",
        page.getByRole("link", { name: /^(Connect|Reconnect)(\s|$)/ }),
      );
      await control(
        "allTools",
        page.getByRole("checkbox", { name: /^All tools$/i }),
      );
    } else {
      await read("projectRouteClassification", async () => {
        const actual = new URL(page.url());
        const project = new URL(this.auth.dashboard.projectURL);
        const base = project.pathname.replace(/\/$/, "");
        return {
          originMatchesProject: actual.origin === project.origin,
          pathnameInRecordedProject:
            actual.pathname === base || actual.pathname.startsWith(base + "/"),
          pathnameIsAssistantList: actual.pathname === base + "/assistants",
          pathnameMatchesRecordedSettings:
            actual.pathname === new URL(this.settingsURL).pathname,
        };
      });
      await read("expandMcpGatewayCount", () =>
        page
          .getByRole("button", { name: "Expand MCP Gateway", exact: true })
          .count(),
      );
      await read("collapseMcpGatewayCount", () =>
        page
          .getByRole("button", { name: "Collapse MCP Gateway", exact: true })
          .count(),
      );
      await read("backToAllServersCount", () =>
        page
          .getByRole("link", { name: "Back to all servers", exact: true })
          .count(),
      );
      await read("assistantsLinkCount", () =>
        page
          .getByRole("link", { name: "Assistants Beta", exact: true })
          .count(),
      );
      await read("assistantsLinkIncludeHiddenCount", () =>
        page
          .getByRole("link", {
            name: "Assistants Beta",
            exact: true,
            includeHidden: true,
          })
          .count(),
      );
      await read("projectNavigationVisible", () =>
        page
          .getByRole("link", { name: "Project Assistant Beta", exact: true })
          .isVisible(),
      );
      await read("identityTabCount", () =>
        page.getByRole("tab", { name: "Identity", exact: true }).count(),
      );
      await read("identityTabIncludeHiddenCount", () =>
        page
          .getByRole("tab", {
            name: "Identity",
            exact: true,
            includeHidden: true,
          })
          .count(),
      );
      await read("openDialogCount", () => page.getByRole("dialog").count());
      await read("rootCount", () => page.locator("#root").count());
      await read(
        "rootTextEmpty",
        async () =>
          (await page.locator("#root").innerText({ timeout: 500 })).trim() ===
          "",
      );
      await read("sectionLoadingVisible", () =>
        page
          .getByText(/^Loading(?:\b|…)/i)
          .filter({ visible: true })
          .count()
          .then((count) => count > 0),
      );
      await read("upstreamCredentialsVisible", () =>
        page.getByText("Upstream credentials", { exact: true }).isVisible(),
      );
    }
    await read("ariaSnapshot", async () =>
      (await page.locator("body").ariaSnapshot({ timeout: 2_000 })).slice(
        0,
        16_384,
      ),
    );
    try {
      const path = test
        .info()
        .outputPath(`private-assistant-${stage}-page-error.json`);
      await mkdir(dirname(path), { recursive: true, mode: 0o700 });
      const file = await open(path, "wx", 0o600);
      try {
        await file.writeFile(JSON.stringify(evidence, null, 2));
      } finally {
        await file.close();
      }
      return true;
    } catch {
      return false;
    }
  }

  private async completeConsent(page: Page, recovery: boolean) {
    const done = recovery
      ? page.getByRole("heading", { name: "Account connected", exact: true })
      : page.getByText("Authentication complete. You can close this window.", {
          exact: true,
        });
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
