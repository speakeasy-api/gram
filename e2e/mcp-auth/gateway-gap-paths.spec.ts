import { randomUUID } from "node:crypto";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import { GatewayActions } from "./gateway-actions.ts";
import { gatewayEchoes } from "./gateway.ts";
import { addPersonalOffer } from "./personal-offer-actions.ts";
import { disableCredential } from "./credential-actions.ts";
import {
  assertGatewayCalls,
  authorizeReviewed,
  connectionStatuses,
  phase,
  reviewConsent,
  serverSlug,
} from "./transition-actions.ts";

test.describe.configure({ mode: "default" });
test.setTimeout(600_000);
const label = (id: string) => `${id.toLowerCase()}-${randomUUID().slice(0, 8)}`;

async function sharedRegistration(auth: AuthScenario, id: string) {
  return test.step(`${id} UI setup: two resources, one exact personal credential`, async () => {
    phase(id, "ui-shared-registration-setup");
    const a = await auth.dashboard.remote(
      label(`${id}-a`),
      auth.mock.profiles.oauthAlpha,
      "user",
    );
    const shared = await auth.dashboard.personalCredentialName();
    const original = auth.client(a.endpoint);
    await auth.authorize(original);
    await auth.invoke(original, auth.mock.profiles.oauthAlpha, "alpha-user");
    const registration = [...auth.mock.events]
      .reverse()
      .find(
        (event) =>
          event.kind === "authorize" &&
          event.resource === auth.mock.profiles.oauthAlpha,
      )?.clientId;
    expect(
      registration,
      `${id}: A's actual OAuth handback identifies its registration`,
    ).toBeTruthy();
    const b = await auth.dashboard.remote(
      label(`${id}-b`),
      auth.mock.profiles.multiMethodTwin,
      "user",
    );
    const other = await auth.dashboard.personalCredentialName();
    if (other !== shared) await disableCredential(auth.page, other);
    await auth.dashboard.attachExistingOffer(shared, a.name, b.name);
    const graph = await auth.graph();
    const rows = [a, b].map((server) =>
      graph.servers.find((row) => row.name === server.name)!,
    );
    expect(rows.every(Boolean), `${id}: both servers in one snapshot`).toBe(
      true,
    );
    const common = rows[0]!.offers
      .filter(
        (offer) =>
          offer.type === "remote_session" &&
          offer.enabled &&
          !offer.deleted &&
          !offer.bindingDeleted &&
          offer.registration !== null,
      )
      .filter((offer) =>
        rows[1]!.offers.some(
          (other) =>
            other.client === offer.client &&
            other.registration === offer.registration &&
            other.enabled &&
            !other.deleted &&
            !other.bindingDeleted,
        ),
      );
    expect(
      common,
      `${id}: one shared typed registration binding across distinct resources`,
    ).toHaveLength(1);
    await test.info().attach(`${id}-legacy-edge-classification`, {
      body: JSON.stringify({
        coverage: "UI-created shared typed registration variant only",
        ambiguousLegacyVariant:
          "not constructed or established by this snapshot",
        runtimeFallbackAbsence: "not established by persisted enforcement",
        members: rows.map((row) => {
          const offer = row.offers.find(
            (candidate) => candidate.client === common[0]!.client,
          )!;
          return {
            server: row.server,
            typedClient: offer.client,
            registration: offer.registration,
            issuer: offer.issuer,
            issuerBridge: offer.issuerBridge,
            directLegacyAssociation: offer.directLegacyAssociation,
            missingEdges: [
              ...(offer.registration === null || offer.registrationDeleted
                ? ["typed-client -> active registration"]
                : []),
              ...(offer.issuer === null || offer.issuerDeleted
                ? ["typed-client -> active issuer"]
                : []),
              ...(offer.issuerBridge === "missing"
                ? ["issuer -> registration bridge"]
                : []),
              ...(offer.directLegacyAssociation === "missing"
                ? ["server -> direct legacy registration association"]
                : []),
            ],
          };
        }),
      }),
      contentType: "application/json",
    });
    // Persisted enforcement and typed edges alone do not prove absence of runtime fallback.
    return { a, b, shared, original, registration };
  });
}

test("G1 G2 UI-created typed shared OAuth registration preserves distinct gateway resource handbacks", async ({
  auth,
}) => {
  const id = "G1-G2";
  const { a, b, shared, registration } = await sharedRegistration(auth, id);
  const gateway = await new GatewayActions(auth).create(label(id), [
    a.name,
    b.name,
  ]);
  await test.step(`${id} gateway consent and exact resource routing`, async () => {
    phase(id, "gateway-consent");
    const client = auth.client(gateway.endpoint);
    const offset = auth.mock.events.length;
    await auth.authorize(client, shared);
    expect(
      auth.mock.events
        .slice(offset)
        .filter(
          (event) =>
            event.kind === "token" &&
            event.resource === auth.mock.profiles.multiMethodTwin,
        ),
      `${id}: B's new resource grant must use A's exact registration`,
    ).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          clientId: registration,
          scopes: ["fixture:read"],
          outcome: "success",
        }),
      ]),
    );
    await assertGatewayCalls(
      auth,
      client,
      [
        {
          ...a,
          resource: auth.mock.profiles.oauthAlpha,
          principal: "alpha-user",
        },
        {
          ...b,
          resource: auth.mock.profiles.multiMethodTwin,
          principal: "alpha-user",
        },
      ],
      id,
    );
    await test.info().attach(`${id}-upstream-handbacks`, {
      body: JSON.stringify(
        auth.mock.events
          .slice(offset)
          .filter((event) =>
            ["authorize", "token", "refresh"].includes(event.kind),
          ),
      ),
      contentType: "application/json",
    });
  });
});

test("G7 B login must not overwrite A resource for existing fresh and refreshed callers", async ({
  auth,
}) => {
  const id = "G7";
  const { a, b, shared, original } = await sharedRegistration(auth, id);
  await test.step(`${id} B signs in to its distinct resource`, async () => {
    phase(id, "b-login");
    const client = auth.client(b.endpoint);
    await auth.authorize(client, shared);
    await auth.invoke(client, auth.mock.profiles.multiMethodTwin, "alpha-user");
  });
  await test.step(`${id} A existing fresh and refreshed positive controls`, async () => {
    phase(id, "a-existing");
    await auth.invoke(original, auth.mock.profiles.oauthAlpha, "alpha-user");
    phase(id, "a-fresh");
    const fresh = auth.client(a.endpoint);
    await auth.authorize(fresh, shared);
    await auth.invoke(fresh, auth.mock.profiles.oauthAlpha, "alpha-user");
    phase(id, "a-refreshed");
    await original.refresh();
    await auth.invoke(original, auth.mock.profiles.oauthAlpha, "alpha-user");
  });
});

test("G25 connection_status classifies each member by its own authentication", async ({
  auth,
}) => {
  const id = "G25";
  phase(id, "ui-mixed-auth-topology");
  const personal = await auth.dashboard.remote(
    label(id),
    auth.mock.profiles.oauthAlpha,
    "user",
  );
  const anonymous = await auth.dashboard.remote(
    label(id),
    auth.mock.profiles.noAuth,
  );
  const gateway = await new GatewayActions(auth).create(label(id), [
    personal.name,
    anonymous.name,
  ]);
  const client = auth.client(gateway.endpoint);
  await authorizeReviewed(auth, client, id, "live");
  await test.step(`${id} status and actual member execution agree`, async () => {
    phase(id, "per-member-status");
    const statuses = await connectionStatuses(client);
    expect(statuses.find((row) => row.name === personal.name)).toMatchObject({
      status: "connected",
    });
    expect(statuses.find((row) => row.name === anonymous.name)).toMatchObject({
      status: "not_required",
    });
    await assertGatewayCalls(
      auth,
      client,
      [
        {
          ...personal,
          resource: auth.mock.profiles.oauthAlpha,
          principal: "alpha-user",
        },
        {
          ...anonymous,
          resource: auth.mock.profiles.noAuth,
          principal: "anonymous",
        },
      ],
      id,
    );
  });
});

test("G9 approved no-auth member requires reviewed consent after administrator adds authentication", async ({
  auth,
}) => {
  const id = "G9";
  const resource = auth.mock.profiles.noAuth;
  phase(id, "ui-no-auth-topology");
  const member = await auth.dashboard.remote(label(id), resource);
  const gateway = await new GatewayActions(auth).create(label(id), [
    member.name,
  ]);
  const client = auth.client(gateway.endpoint);
  await auth.authorize(client);
  await assertGatewayCalls(
    auth,
    client,
    [{ ...member, resource, principal: "anonymous" }],
    id,
  );
  // Capture the exact qualified call while the original anonymous grant works;
  // rediscovery after mutation must not substitute a different member or tool.
  const staleCalls = await gatewayEchoes(client, [serverSlug(member)]);
  expect(staleCalls).toHaveLength(1);
  const staleCall = staleCalls[0]!;
  await test.step(`${id} administrator adds a real personal authentication offer`, async () => {
    phase(id, "admin-adds-auth");
    auth.mock.setResourceAuth(resource, true);
    const offset = auth.mock.events.length;
    // Observe the actual UI commit, without issuing a separate API request.
    const [commit] = await Promise.all([
      auth.page
        .waitForResponse(
          (response) =>
            response.request().method() === "POST" &&
            new URL(response.url()).pathname ===
              "/rpc/remoteSessions.commitServerIdentityConfiguration",
        )
        .then(async (response) => ({
          ok: response.ok(),
          body: (await response.json()) as {
            status?: unknown;
            registration_method?: unknown;
            client?: { id?: unknown; client_id?: unknown };
          } | null,
        }))
        .catch(() => ({ ok: false, body: null })),
      auth.dashboard.remote(
        label(`${id}-registration`),
        auth.mock.profiles.oauthAlpha,
        "user",
      ),
    ]);
    const registrations = auth.mock.events
      .slice(offset)
      .filter((event) => event.kind === "register");
    expect(registrations.length, `${id}: exactly one new registration`).toBe(1);
    expect(
      registrations[0]!.provider === "alpha" &&
        typeof registrations[0]!.clientId === "string" &&
        registrations[0]!.clientId.length > 0,
      `${id}: registration must belong to the expected provider`,
    ).toBe(true);
    const registrationRowId = commit.body?.client?.id;
    expect(
      commit.ok &&
        commit.body?.status === "registered" &&
        commit.body?.registration_method === "dcr" &&
        commit.body?.client?.client_id === registrations[0]!.clientId &&
        typeof registrationRowId === "string" &&
        /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
          registrationRowId,
        ),
      `${id}: UI commit must identify the exact recorded DCR registration`,
    ).toBe(true);
    if (typeof registrationRowId !== "string")
      throw new Error(`${id}: UI commit did not provide a registration row`);
    await auth.page.goto(`${member.serverURL}/settings`);
    await addPersonalOffer(
      auth.page,
      label(`${id}-personal`),
      registrations[0]!.clientId!,
      registrationRowId,
    );
  });
  await test.step(`${id} old approval cannot silently authorize the new policy`, async () => {
    phase(id, "old-approval-needs-consent");
    const statuses = await connectionStatuses(client);
    // Keep the classification mismatch red while measuring ordinary recovery.
    expect
      .soft(statuses.find((row) => row.name === member.name))
      .toMatchObject({
        status: "needs_consent",
      });
    const upstreamOffset = auth.mock.events.length;
    const protocolOffset = client.transcript.length;
    let refused: Awaited<ReturnType<typeof staleCall.call>>;
    try {
      refused = await staleCall.call({ marker: randomUUID() });
    } finally {
      // Even rejected upstream attempts violate the stale-call boundary.
      expect(
        auth.mock.events
          .slice(upstreamOffset)
          .filter((event) => event.method === "tools/call"),
        `${id}: stale approval must not forward any upstream tool call`,
      ).toHaveLength(0);
    }
    const attempts = client.transcript.slice(protocolOffset);
    expect(attempts).toHaveLength(1);
    expect(attempts[0]).toMatchObject({ operation: "tools/call", status: 200 });
    // These exact remedies are the stale-choice contract, not generic outage
    // errors. The soft status assertion above still requires needs_consent.
    const remedies = [
      `Server "${staleCall.server}"'s sign-in setup changed. Reconnect this gateway in your AI app and approve the new connection.`,
      `Server "${staleCall.server}" needs your administrator to finish setup or give you access.`,
    ];
    expect(refused).toMatchObject({ isError: true });
    if (
      !refused ||
      typeof refused !== "object" ||
      !("content" in refused) ||
      !Array.isArray(refused.content)
    )
      throw new Error(`${id}: stale refusal must contain text content`);
    expect(refused.content).toHaveLength(1);
    const text = refused.content[0] as {
      type?: unknown;
      text?: unknown;
    } | null;
    expect(
      text?.type === "text" &&
        typeof text.text === "string" &&
        remedies.includes(text.text),
      `${id}: stale call must return the exact member-specific authorization remedy`,
    ).toBe(true);
  });
  await test.step(`${id} explicit new review authorizes the changed member`, async () => {
    phase(id, "explicit-reconsent");
    await authorizeReviewed(auth, client, id);
    await assertGatewayCalls(
      auth,
      client,
      [{ ...member, resource, principal: "alpha-user" }],
      id,
    );
  });
});

test("G28 stale gateway membership review never grants execution on the removed member", async ({
  auth,
}) => {
  const id = "G28";
  const a = await auth.dashboard.remote(label(id), auth.mock.profiles.noAuth);
  const b = await auth.dashboard.remote(label(id), auth.mock.profiles.noAuth);
  const actions = new GatewayActions(auth);
  const gateway = await actions.create(label(id), [a.name, b.name]);
  const baseline = auth.client(gateway.endpoint);
  await auth.authorize(baseline);
  const originalTools = await gatewayEchoes(baseline, [
    serverSlug(a),
    serverSlug(b),
  ]);
  const client = auth.client(gateway.endpoint);
  const flow = await client.beginAuthorization();
  const consent = await auth.page.context().newPage();
  let issued = false;
  try {
    await reviewConsent(consent, flow.authorizationUrl, id);
    await actions.remove(gateway, b.name);
    phase(id, "stale-membership-submit");
    const pending = consent.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.request().isNavigationRequest(),
    );
    await consent
      .getByRole("button", { name: "Give access", exact: true })
      .click();
    const response = await pending;
    const redirect = new URL(
      response.headers()["location"] ?? consent.url(),
      consent.url(),
    );
    if (response.status() === 303 && redirect.searchParams.has("code")) {
      // Permission reduction need not be a conflict if the resulting grant is
      // restricted. Measure access instead of requiring the report's 409.
      await flow.complete();
      auth.trackGrant(client);
      issued = true;
      await client.initialize();
      const offset = auth.mock.events.length;
      const { tools } = await client.listTools();
      const name = originalTools[1]!.qualifiedName;
      const result = await (
        tools.some((tool) => tool.name === "execute_tool")
          ? client.callTool("execute_tool", {
              name,
              arguments: { marker: "g28-removed-member" },
            })
          : client.callTool(name, { marker: "g28-removed-member" })
      ).then(
        (result) => ({ result, error: undefined }),
        (error: unknown) => ({ result: undefined, error: String(error) }),
      );
      if (result.error !== undefined)
        expect(result.error).toMatch(
          /401|403|404|JSON-RPC error|forbidden|not found/i,
        );
      else
        expect(
          result.result,
          "G28: stale approval must not execute the removed member",
        ).toMatchObject({ isError: true });
      expect(
        auth.mock.events
          .slice(offset)
          .filter(
            (event) =>
              event.method === "tools/call" && event.outcome === "authorized",
          ),
      ).toHaveLength(0);
      await assertGatewayCalls(
        auth,
        client,
        [{ ...a, resource: auth.mock.profiles.noAuth, principal: "anonymous" }],
        id,
      );
    } else {
      expect([409, 412]).toContain(response.status());
      expect(response.headers()["content-type"]).toContain("text/html");
      await expect(consent.locator("body")).not.toHaveText(/^\s*\{/);
      await expect(consent.locator("body")).toContainText(
        /reload|review|changed|no longer/i,
      );
    }
  } finally {
    if (issued) await auth.revoke(client);
    await flow.cancel();
    await consent.close();
  }
  const recovered = auth.client(gateway.endpoint);
  await authorizeReviewed(auth, recovered, id);
  await assertGatewayCalls(
    auth,
    recovered,
    [{ ...a, resource: auth.mock.profiles.noAuth, principal: "anonymous" }],
    id,
  );
});

test("G28 direct backend change honors live all-tools approval or renders stale-review recovery", async ({
  auth,
}) => {
  const id = "G28";
  const server = await auth.dashboard.remote(
    label(id),
    auth.mock.profiles.noAuth,
  );
  const caller = auth.client(server.endpoint);
  const flow = await caller.beginAuthorization();
  const consent = await auth.page.context().newPage();
  try {
    await reviewConsent(consent, flow.authorizationUrl, id);
    const reviewOrigin = new URL(consent.url()).origin;
    // Direct consent defaults to a live all-tools grant, not a frozen backend
    // snapshot. Keep that scope and measure routing if approval is accepted.
    await expect(
      consent.locator('input[name="consent-tool-access-mode"][value="all"]'),
    ).toBeChecked();
    await test.step(
      "G28 administrator changes the actual upstream URL through Settings",
      async () => {
        await auth.page.goto(`${server.serverURL}/settings`);
        const field = auth.page
          .locator(".gram-elements")
          .getByLabel("Remote URL", { exact: true });
        await field.fill(auth.mock.profiles.noAuthTwin);
        const general = auth.page.locator("section").filter({
          has: auth.page.getByRole("heading", {
            name: "General",
            exact: true,
          }),
        });
        await general
          .getByRole("button", { name: "Verify connectivity", exact: true })
          .click();
        const save = general.getByRole("button", { name: "Save", exact: true });
        await expect(save).toBeEnabled();
        await save.click();
        await expect(save).toBeDisabled();
        await auth.page.reload();
        await expect(
          auth.page.getByLabel("Remote URL", { exact: true }),
        ).toHaveValue(auth.mock.profiles.noAuthTwin);
      },
      { timeout: 45_000 },
    );
    phase(id, "stale-backend-submit");
    const pending = consent.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.request().isNavigationRequest(),
    );
    await consent
      .getByRole("button", { name: "Give access", exact: true })
      .click();
    const response = await pending;
    if (response.status() === 303) {
      const location = new URL(response.headers()["location"]!, consent.url());
      if (location.searchParams.has("code")) {
        // A live grant need not reject a URL-only change with the same no-auth
        // selection. Completion validates the callback; invocation must reach
        // only the newly configured resource with the unchanged identity.
        await flow.complete();
        auth.trackGrant(caller);
        await caller.initialize();
        await auth.invoke(caller, auth.mock.profiles.noAuthTwin, "anonymous");
      } else {
        expect(location.origin).toBe(reviewOrigin);
        await expect(
          consent.getByRole("button", { name: "Give access", exact: true }),
        ).toBeVisible();
      }
    } else {
      expect([409, 412]).toContain(response.status());
      expect(
        response.headers()["content-type"],
        "G28: browser conflicts must render review, not raw JSON",
      ).toContain("text/html");
      await expect(consent.locator("body")).not.toHaveText(/^\s*\{/);
      await expect(consent.locator("body")).toContainText(
        /reload|review|changed|no longer/i,
      );
    }
  } finally {
    await flow.cancel();
    await consent.close();
  }
  phase(id, "fresh-backend-review");
  const recovered = auth.client(server.endpoint);
  await auth.authorize(recovered);
  await auth.invoke(recovered, auth.mock.profiles.noAuthTwin, "anonymous");
});
