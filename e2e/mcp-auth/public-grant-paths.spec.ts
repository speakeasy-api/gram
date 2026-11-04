import { test, expect, type AuthScenario } from "./fixtures.ts";
import { createOAuthRegistration } from "./auth-setup-actions.ts";
import { addPersonalOffer } from "./personal-offer-actions.ts";
import { TunnelActions } from "./tunnel-actions.ts";

// Native tunnel creation reveals a one-time key. Do not capture that page.
test.use({ trace: "off", screenshot: "off", video: "off" });
test.setTimeout(360_000);

async function anonymousRefusal(
  auth: AuthScenario,
  endpoint: string,
  resource: string,
) {
  const offset = auth.mock.events.length;
  // A protocol request, not a management API: fresh anonymous initialize with
  // neither an inbound bearer nor a session ID from the authenticated baseline.
  const response = await fetch(endpoint, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
    },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "initialize",
      params: {
        protocolVersion: "2025-03-26",
        capabilities: {},
        clientInfo: { name: "g15-anonymous", version: "1" },
      },
    }),
    redirect: "error",
    signal: AbortSignal.timeout(15_000),
  });
  expect(response.status).toBe(401);
  expect(await response.json()).toEqual({ error: "unauthorized" });
  // Public tunnels intentionally have no downstream authorization server;
  // relaying the backend's challenge would send callers to the wrong issuer.
  expect(response.headers.get("www-authenticate")).toBeNull();
  const events = auth.mock.events.slice(offset);
  const upstream = events.filter((event) => event.kind === "mcp");
  expect(upstream).toHaveLength(1);
  expect(upstream[0]).toMatchObject({
    resource,
    method: "initialize",
    outcome: "unauthorized",
  });
  expect(upstream[0]!.headerNames).not.toContain("authorization");
  expect(
    events.some(
      (event) =>
        event.principal === "anonymous" || event.outcome === "authorized",
    ),
  ).toBe(false);
  expect(
    events.filter((event) =>
      ["authorize", "token", "refresh", "client_credentials"].includes(
        event.kind,
      ),
    ),
  ).toHaveLength(0);
}

test("G15: public native tunnel never borrows a live or revoked personal upstream grant", async ({
  auth,
}) => {
  const tunnel = new TunnelActions(auth);
  try {
    const registration = await createOAuthRegistration(auth);
    const resource = auth.mock.profiles.oauthAlpha;
    const source =
      await test.step("G15 UI: create private native tunnel with explicit OAuth resource", async () =>
        tunnel.create("fixture-g15-public-grant", resource, resource));
    await test.step("G15 UI: attach the named registration and establish a real personal grant", async () => {
      await addPersonalOffer(auth.page, registration.name, registration.name);
      const signedIn = auth.client(source.endpoint);
      await auth.authorize(signedIn);
      await auth.invoke(signedIn, resource, "alpha-user");
      expect(
        auth.mock.events.some(
          (event) =>
            event.kind === "token" &&
            event.clientId === registration.clientId &&
            event.resource === resource &&
            event.outcome === "success",
        ),
      ).toBe(true);
    });
    await test.step("G15 UI: source-owner public consent precedes server Public visibility", async () => {
      const { page } = auth;
      await page.goto(`${source.serverURL}/settings`);
      await page.getByRole("button", { name: "Private", exact: true }).click();
      await expect(
        page.getByRole("menuitem", { name: /^Public/ }),
      ).toBeDisabled();
      await page.keyboard.press("Escape");
      await page
        .getByRole("button", { name: "Enable public access", exact: true })
        .click();
      const confirmation = page.getByRole("dialog", {
        name: "Enable public anonymous access?",
        exact: true,
      });
      await confirmation
        .getByRole("textbox", {
          name: "Type ALLOW PUBLIC ACCESS to confirm",
          exact: true,
        })
        .fill("ALLOW PUBLIC ACCESS");
      await confirmation
        .getByRole("button", { name: "Enable public access", exact: true })
        .click();
      await expect(confirmation).not.toBeVisible();
      await page.getByRole("button", { name: "Private", exact: true }).click();
      const publicOption = page.getByRole("menuitem", { name: /^Public/ });
      await expect(publicOption).toBeEnabled();
      await publicOption.click();
      await expect(
        page.getByRole("button", { name: "Public", exact: true }),
      ).toBeVisible();
      await page.reload();
      await expect(
        page.getByRole("button", { name: "Public", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("button", {
          name: "Disable public access",
          exact: true,
        }),
      ).toBeVisible();
    });
    await test.step("G15 protocol: anonymous caller cannot borrow even the live personal grant", async () => {
      await anonymousRefusal(auth, source.endpoint, resource);
    });
    await test.step("G15 protocol: upstream revocation without downstream revoke remains a bounded refusal", async () => {
      auth.mock.revokeTokens();
      await anonymousRefusal(auth, source.endpoint, resource);
      await anonymousRefusal(auth, source.endpoint, resource);
    });
  } finally {
    await tunnel.close();
  }
});
