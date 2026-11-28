import { randomUUID } from "node:crypto";
import { writeFile } from "node:fs/promises";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import type { FixtureMcpClient } from "./client.ts";
import { GatewayActions } from "./gateway-actions.ts";
import { createOAuthRegistration } from "./auth-setup-actions.ts";
import { addPersonalOffer } from "./personal-offer-actions.ts";
import {
  authorizeReviewed,
  phase,
  reviewConsent,
} from "./transition-actions.ts";

// fixtures.ts verifies persisted enforcement before any topology is created.
test.describe.configure({ mode: "default" });
test.setTimeout(600_000);
const id = "G9-code-mode";
const label = () => `${id}-${randomUUID().slice(0, 8)}`;

function record(value: unknown): Record<string, unknown> {
  expect(value).not.toBeNull();
  expect(typeof value).toBe("object");
  expect(Array.isArray(value)).toBe(false);
  return value as Record<string, unknown>;
}

async function execute(
  client: FixtureMcpClient,
  code: string,
  onConnectAdminRefusal?: () => Promise<void>,
) {
  const result = record(await client.callTool("execute", { code }));
  const execution = record(result.structuredContent);
  if (result.isError === true || execution.error !== undefined) {
    const error =
      execution.error !== null && typeof execution.error === "object"
        ? (execution.error as Record<string, unknown>)
        : {};
    // Runner messages can contain program values. Never attach raw messages,
    // source, output, tool paths, recovery URLs, or the complete result.
    const knownCodes = [
      "execution_failed",
      "runtime_unavailable",
      "invalid_program",
      "capacity_exceeded",
      "deadline_exceeded",
      "cancelled",
      "tool_scope_unavailable",
      "authorization_revoked",
      "outcome_unknown",
      "callback_limit",
      "protocol_error",
      "admission_refused",
      "request_active",
      "not_admitted",
    ];
    const genericMessages = [
      "tool operation unavailable or arguments invalid",
      "this server changed since consent; the user must reconnect this gateway in their AI app and approve it",
      "code runtime unavailable; program was not submitted",
      "needs your administrator to finish setup or give you access.",
    ];
    const message = typeof error.message === "string" ? error.message : "";
    const messageMarkers = [
      "AttributeError",
      "TypeError",
      "NameError",
      "KeyError",
      "SyntaxError",
      "connect",
      "keyword",
      "not found",
      "no attribute",
      "unavailable",
      "not permitted",
      "consent",
      "sign-in",
      "authorization",
      "connection_required",
    ].filter((marker) => message.toLowerCase().includes(marker.toLowerCase()));
    // Keep the complete exception private for local source correlation. It can
    // contain program values or a one-time recovery URL, so never attach it to
    // the report or include it in the thrown diagnostic. Output is ignored.
    let privateCaptureWritten = false;
    try {
      await writeFile(
        test.info().outputPath("private-code-mode-error.json"),
        JSON.stringify({ message }),
        { mode: 0o600 },
      );
      privateCaptureWritten = true;
    } catch {
      // Diagnostic storage must not replace the original execution failure.
    }
    const diagnostic = {
      privateCaptureWritten,
      messageLength: message.length,
      messageMarkers,
      operation:
        /^await tools\.(call|servers|search|describe|connect)\(/.exec(
          code,
        )?.[1] ?? "unknown",
      code:
        typeof error.code === "string" && knownCodes.includes(error.code)
          ? error.code
          : "unrecognized_or_missing",
      message:
        genericMessages.find(
          (message) =>
            typeof error.message === "string" &&
            error.message.includes(message),
        ) ?? "non-generic message withheld",
    };
    await test.info().attach("code-mode-execution-failure", {
      body: JSON.stringify(diagnostic),
      contentType: "application/json",
    });
    const failure = new Error(
      `CODE_MODE_EXECUTION: ${JSON.stringify(diagnostic)}`,
    );
    if (
      diagnostic.operation === "connect" &&
      error.code === "execution_failed" &&
      /RuntimeError: Server [^\r\n]+ needs your administrator to finish setup or give you access\.(?:\r?\n|$)/.test(
        message,
      ) &&
      onConnectAdminRefusal
    ) {
      try {
        await onConnectAdminRefusal();
      } catch {
        // Even diagnostic attachment failure must preserve the original error.
      }
    }
    throw failure;
  }
  return execution.value;
}

// Independent ordinary-consent control, never approval or tools.connect recovery.
async function probeOrdinaryConsent(
  auth: AuthScenario,
  client: FixtureMcpClient,
  registration: Awaited<ReturnType<typeof createOAuthRegistration>>,
  resource: string,
) {
  phase(id, "diagnostic-only:ordinary-consent-after-connect-admin-refusal");
  const offset = auth.mock.events.length;
  const observed = {
    flowStarted: false,
    navigationCompleted: false,
    exactRegistrationCard: false,
    exactRegistrationGatewayForm: false,
    usableConnect: false,
    mockApprovalVisible: false,
    exactRegistrationApprovalVisible: false,
    exactProviderRequest: false,
    providerResourceMatches: false,
    providerScopesMatch: false,
    permissionBlockedUI: false,
    providerInvalidTarget: false,
    observationFailed: false,
    flowCancelled: false,
    pageClosed: false,
  };
  let flow:
    | Awaited<ReturnType<FixtureMcpClient["beginAuthorization"]>>
    | undefined;
  let page: import("@playwright/test").Page | undefined;
  try {
    flow = await client.beginAuthorization();
    observed.flowStarted = true;
    page = await auth.page.context().newPage();
    const providerURL = new URL(
      `${auth.mock.providers.alpha.issuer}/authorize`,
    );
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (
        request.method() !== "GET" ||
        url.origin !== providerURL.origin ||
        url.pathname !== providerURL.pathname ||
        url.searchParams.get("client_id") !== registration.clientId
      )
        return;
      observed.exactProviderRequest = true;
      observed.providerResourceMatches =
        url.searchParams.get("resource") === resource;
      observed.providerScopesMatch =
        JSON.stringify(
          (url.searchParams.get("scope") ?? "")
            .split(/\s+/)
            .filter(Boolean)
            .sort(),
        ) === JSON.stringify([...registration.scopes].sort());
    });
    // The UI client detail route names the registration used by consent cards.
    // Its ID stays local; only an exact-match boolean enters the diagnostic.
    const registrationID = /\/clients\/([^/]+)(?:\/|$)/.exec(
      new URL(registration.url).pathname,
    )?.[1];
    if (!registrationID || !/^[a-f0-9-]{36}$/i.test(registrationID)) {
      throw new Error(
        "ORDINARY_CONSENT_DIAGNOSTIC: registration route unavailable",
      );
    }
    const exactCard = page.locator(
      `li[data-remote-client="${registrationID}"]`,
    );
    const approval = page.getByRole("button", {
      name: "Approve mock access",
      exact: true,
    });
    await page.goto(flow.authorizationUrl, {
      waitUntil: "domcontentloaded",
      timeout: 20_000,
    });
    observed.navigationCompleted = true;
    await expect
      .poll(
        async () => {
          observed.exactRegistrationCard = (await exactCard.count()) === 1;
          const connect = exactCard.locator("[data-connect-link]");
          observed.usableConnect =
            observed.exactRegistrationCard &&
            (await connect.count()) === 1 &&
            (await connect.isVisible()) &&
            (await connect.isEnabled());
          // Gateway consent can render the registration in a server action
          // instead of a remote-session card. Observe only; never connect here.
          const gatewayForms = page!.locator(
            "[data-gateway-server-actions] form",
          );
          let matchingForms = 0;
          let matchingIndex = -1;
          const formCount = await gatewayForms.count();
          for (let index = 0; index < formCount; index++) {
            const client = gatewayForms
              .nth(index)
              .locator('input[type="hidden"][name="client_id"]');
            if (
              (await client.count()) === 1 &&
              (await client.inputValue()) === registrationID
            ) {
              matchingForms++;
              matchingIndex = index;
            }
          }
          observed.exactRegistrationGatewayForm = matchingForms === 1;
          if (observed.exactRegistrationGatewayForm) {
            const gatewayConnect = gatewayForms
              .nth(matchingIndex)
              .locator('[data-connect-link][name="action"][value="connect"]');
            const usableGatewayConnect =
              (await gatewayConnect.count()) === 1 &&
              (await gatewayConnect.isVisible()) &&
              (await gatewayConnect.isEnabled());
            observed.usableConnect ||= usableGatewayConnect;
          }
          observed.mockApprovalVisible = await approval.isVisible();
          observed.exactRegistrationApprovalVisible =
            observed.mockApprovalVisible && observed.exactProviderRequest;
          observed.permissionBlockedUI =
            (await page!
              .getByText("No access", { exact: true })
              .first()
              .isVisible()) ||
            (await page!
              .getByText(
                "You don't have access to any servers on this gateway yet.",
                { exact: true },
              )
              .isVisible());
          observed.providerInvalidTarget = auth.mock.events
            .slice(offset)
            .some(
              (event) =>
                event.kind === "authorize" &&
                event.provider === "alpha" &&
                event.clientId === registration.clientId &&
                event.outcome === "invalid_target",
            );
          return (
            observed.usableConnect ||
            observed.exactRegistrationApprovalVisible ||
            observed.permissionBlockedUI ||
            observed.providerInvalidTarget
          );
        },
        {
          timeout: 10_000,
          message: "ORDINARY_CONSENT_DIAGNOSTIC: observe without approving",
        },
      )
      .toBe(true);
  } catch {
    // Optional control errors are unknown, not evidence of a product failure.
    // Never disclose URL-bearing browser errors or replace the connect failure.
    observed.observationFailed = true;
  } finally {
    try {
      if (flow) {
        await flow.cancel();
        observed.flowCancelled = true;
      }
    } catch {
      observed.observationFailed = true;
    }
    try {
      if (page) {
        if (!page.isClosed()) await page.close();
        observed.pageClosed = true;
      }
    } catch {
      observed.observationFailed = true;
    }
    const journal = auth.mock.events
      .slice(offset)
      .filter(
        (event) =>
          event.kind === "authorize" &&
          event.provider === "alpha" &&
          event.clientId === registration.clientId,
      )
      .map((event) => ({
        kind: "authorize",
        provider: "alpha",
        clientId: registration.clientId,
        resource:
          event.resource === resource
            ? resource
            : event.resource === ""
              ? ""
              : "withheld_nonmatching_resource",
        scopes: (event.scopes ?? []).filter((scope) =>
          registration.scopes.includes(scope),
        ),
        scopesMatch:
          JSON.stringify([...(event.scopes ?? [])].sort()) ===
          JSON.stringify([...registration.scopes].sort()),
        outcome: [
          "invalid_target",
          "invalid_scope",
          "invalid_request",
          "unsupported_response_type",
          "access_denied",
          "success",
        ].includes(event.outcome ?? "")
          ? event.outcome
          : "unrecognized",
      }));
    observed.providerInvalidTarget = journal.some(
      (event) => event.outcome === "invalid_target",
    );
    await test.info().attach("code-mode-independent-ordinary-consent-control", {
      body: JSON.stringify({
        diagnosticOnly: true,
        ...observed,
        // mock-server.ts rejects invalid resources in /authorize before showing
        // approval. Reaching that exact registration is forwarding evidence,
        // not a completed connection or a successful resource contract.
        providerRejectionBoundary: observed.providerInvalidTarget
          ? "mock_authorize_validResource_invalid_target"
          : "not_established",
        journal,
      }),
      contentType: "application/json",
    });
  }
}

async function echo(
  auth: AuthScenario,
  client: FixtureMcpClient,
  path: string,
  resource: string,
  principal: string,
) {
  const marker = randomUUID();
  const offset = auth.mock.events.length;
  const result = record(
    await execute(
      client,
      `await tools.call(${JSON.stringify(path)}, {"marker": ${JSON.stringify(marker)}})`,
    ),
  );
  expect(result.ok).toBe(true);
  expect(JSON.stringify(result)).toContain(marker);
  const calls = auth.mock.events
    .slice(offset)
    .filter((event) => event.method === "tools/call");
  expect(calls).toHaveLength(1);
  expect(calls[0]).toMatchObject({
    resource,
    principal,
    outcome: "authorized",
  });
}

test("G9 Code Mode: frozen no-auth catalog needs explicit review when sign-in is added", async ({
  auth,
}) => {
  const resource = auth.mock.profiles.noAuth;
  const { member, gateway } = await test.step(
    "UI: create a stored gateway and persist Code Mode discovery",
    async () => {
      phase(id, "ui-code-mode-topology");
      const member = await auth.dashboard.remote(label(), resource);
      const gateway = await new GatewayActions(auth).create(label(), [
        member.name,
      ]);
      // GatewayDiscoverySection persists the default through its real Save button.
      // Do not override the endpoint mode or change product flags behind the UI.
      // GatewayActions.create already leaves the mounted settings page open.
      await expect(auth.page).toHaveURL(`${gateway.url}/settings`);
      const section = auth.page.locator("section#discovery");
      const mode = section.getByRole("combobox", {
        name: "Default mode",
        exact: true,
      });
      await mode.click();
      const option = auth.page.getByRole("option", {
        name: "Code Mode",
        exact: true,
      });
      await expect(
        option,
        "Code Mode requires organization entitlement and an available configured runtime",
      ).toBeEnabled();
      await option.click();
      await section.getByRole("button", { name: "Save", exact: true }).click();
      await expect(
        auth.page.getByText("Gateway discovery updated", { exact: true }),
      ).toBeVisible();
      await auth.page.reload();
      await expect(mode).toHaveText("Code Mode");
      return { member, gateway };
    },
    { timeout: 180_000 },
  );
  const client = auth.client(gateway.endpoint);
  const { path, slug } = await test.step(
    "Approve a frozen no-auth catalog and execute the upstream echo through Python",
    async () => {
      phase(id, "initial-code-execution");
      // Discovery mode and consent catalog mode are independent. This helper
      // selects and verifies the real cached radio before submitting approval.
      await authorizeReviewed(auth, client, id, "frozen");
      const { tools } = await client.listTools();
      expect(tools.map((tool) => tool.name)).toEqual(["execute"]);
      expect(tools[0]!.inputSchema).toMatchObject({ required: ["code"] });
      const servers = record(await execute(client, "await tools.servers()"));
      expect(Array.isArray(servers.items)).toBe(true);
      const members = (servers.items as unknown[]).map(record);
      expect(members).toHaveLength(1);
      expect(members[0]!.name).toBe(member.name);
      const slug = members[0]!.slug;
      expect(typeof slug).toBe("string");
      const search = record(
        await execute(
          client,
          `await tools.search("fixture_echo", server=${JSON.stringify(slug)})`,
        ),
      );
      expect(search.incomplete).toBe(false);
      expect(Array.isArray(search.items)).toBe(true);
      const echoes = (search.items as unknown[])
        .map(record)
        .filter(
          (item) =>
            typeof item.path === "string" &&
            item.path.endsWith("--fixture_echo"),
        );
      expect(echoes).toHaveLength(1);
      const path = echoes[0]!.path as string;
      const description = record(
        await execute(client, `await tools.describe(${JSON.stringify(path)})`),
      );
      expect(description.path).toBe(path);
      expect(record(description.definition).inputSchema).toBeTruthy();
      await echo(auth, client, path, resource, "anonymous");
      return { path, slug };
    },
    { timeout: 120_000 },
  );
  const registration = await test.step(
    "Administrator adds a typed personal sign-in offer to the same resource",
    async () => {
      phase(id, "admin-adds-auth");
      auth.mock.setResourceAuth(resource, true);
      const registration = await createOAuthRegistration(auth);
      await auth.page.goto(`${member.serverURL}/settings`);
      await addPersonalOffer(auth.page, label(), registration.name);
      phase(id, "typed-offer-persisted-before-recovery");
      return registration;
    },
    { timeout: 90_000 },
  );
  const authorizationURL = await test.step(
    "Old Code Mode approval refuses execution and supplies actionable reconsent",
    async () => {
      phase(id, "old-code-approval-needs-consent");
      const offset = auth.mock.events.length;
      // This is an actual attempted Code Mode call, not a status-only assertion.
      const denied = record(
        await client.callTool("execute", {
          code: `await tools.call(${JSON.stringify(path)}, {"marker": "must-not-execute"})`,
        }),
      );
      expect(denied.isError).toBe(true);
      expect(record(denied.structuredContent).tool_calls).toEqual([
        { path, outcome: "not_dispatched" },
      ]);
      expect(
        auth.mock.events
          .slice(offset)
          .filter((event) => event.method === "tools/call"),
      ).toHaveLength(0);
      // A typed auth gap is sanitized as unavailable; connect carries recovery.
      expect(record(record(denied.structuredContent).error).code).toBe(
        "execution_failed",
      );
      const recovery = record(
        await execute(
          client,
          `await tools.connect(server=${JSON.stringify(slug)})`,
          () =>
            probeOrdinaryConsent(
              auth,
              auth.client(gateway.endpoint),
              registration,
              resource,
            ),
        ),
      );
      expect(recovery.status).toBe("authorization_required");
      // Recovery can require either catalog reconsent or upstream sign-in.
      expect(["consent_required", "upstream_sign_in"]).toContain(
        recovery.reason,
      );
      expect(typeof recovery.authorization_url).toBe("string");
      expect(new URL(recovery.authorization_url as string).protocol).toMatch(
        /^https?:$/,
      );
      expect(recovery.instructions).toEqual(expect.any(String));
      expect((recovery.instructions as string).length).toBeGreaterThan(0);
      expect(
        auth.mock.events
          .slice(offset)
          .filter((event) => event.method === "tools/call"),
      ).toHaveLength(0);
      return recovery.authorization_url as string;
    },
    { timeout: 90_000 },
  );
  await test.step(
    "Follow the recovery URL and explicitly approve the refreshed frozen catalog",
    async () => {
      phase(id, "explicit-code-reconsent");
      // Exercise tools.connect's actual ticket, not an unrelated fresh OAuth
      // authorization. Frozen reconnect updates this existing connection.
      // First-party completion closes its tab; keep the dashboard available
      // for fixture cleanup while retaining the same signed-in browser context.
      const recoveryPage = await auth.page.context().newPage();
      try {
        await reviewConsent(recoveryPage, authorizationURL, id);
        const frozen = recoveryPage.locator(
          'input[name="consent-tool-access-mode"][value="cached"]',
        );
        await frozen.focus();
        await frozen.press("Space");
        await expect(frozen).toBeChecked();
        await recoveryPage
          .getByRole("button", { name: "Give access", exact: true })
          .click();
        await expect(
          recoveryPage.getByText(
            "Connection complete. This tab will close automatically.",
            { exact: true },
          ),
        ).toBeVisible();
      } finally {
        if (!recoveryPage.isClosed()) await recoveryPage.close();
      }
      await echo(auth, client, path, resource, "alpha-user");
    },
    { timeout: 120_000 },
  );
});
