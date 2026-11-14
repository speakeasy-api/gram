import { randomUUID } from "node:crypto";
import { test as base, expect } from "./fixtures.ts";
import { TunnelActions } from "./tunnel-actions.ts";

const test = base.extend<{ tunnel: TunnelActions }>({
  tunnel: async ({ auth }, use) => {
    const tunnel = new TunnelActions(auth);
    try {
      await use(tunnel);
    } finally {
      await tunnel.close();
    }
  },
});
test.describe.configure({ mode: "default" });
test.setTimeout(300_000);
// Native tunnel creation displays a one-time credential.
test.use({ trace: "off", screenshot: "off", video: "off" });

// Header-only tunnel offers are rejected by the supported product contract.
// Keep the independent unsigned-sibling runtime isolation scenarios.
for (const kind of ["remote", "tunnel"] as const) {
  test(`G8: configuring personal OAuth leaves an existing unsigned ${kind} sibling anonymous`, async ({
    auth,
    tunnel,
  }) => {
    test.info().annotations.push({ type: "gap-hypothesis", description: "G8" });
    const unsigned =
      kind === "remote"
        ? await auth.dashboard.remote(
            `fixture-unsigned-${randomUUID().slice(0, 8)}`,
            auth.mock.profiles.noAuth,
          )
        : await tunnel.create(
            `fixture-unsigned-${randomUUID().slice(0, 8)}`,
            auth.mock.profiles.noAuth,
          );
    test.info().annotations.push({
      type: "auth-stage",
      description: "unsigned-A-created-before-login",
    });
    await auth.graph();
    const existing = auth.client(unsigned.endpoint);
    await existing.initialize();
    await auth.invoke(existing, auth.mock.profiles.noAuth, "anonymous");
    test.info().annotations.push({
      type: "auth-stage",
      description: "unsigned-A-logged-in",
    });
    const before = (await auth.graph()).servers.find(
      (server) => server.name === unsigned.name,
    )!;
    const signed = await auth.dashboard.remote(
      `fixture-signed-${randomUUID().slice(0, 8)}`,
      auth.mock.profiles.oauthAlpha,
      "user",
    );
    test.info().annotations.push({
      type: "auth-stage",
      description: "signed-B-saved-before-login",
    });
    const after = (await auth.graph()).servers.find(
      (server) => server.name === unsigned.name,
    )!;
    expect
      .soft(
        after.signInIssuer,
        "G8: configuring another server must preserve downstream sign-in policy",
      )
      .toBe(before.signInIssuer);
    expect
      .soft(
        after.derivedUpstreamIssuer,
        "G8: an unsigned sibling must not inherit another server's provider",
      )
      .toBe(before.derivedUpstreamIssuer);
    async function measureAnonymous(stage: string) {
      test.info().annotations.push({ type: "auth-stage", description: stage });
      const offset = auth.mock.events.length;
      let refused = false;
      for (const session of ["old", "fresh"] as const) {
        await test.step(
          `${stage}: ${session} unsigned session`,
          async () => {
            let outcome = "authorized";
            try {
              const client =
                session === "old" ? existing : auth.client(unsigned.endpoint);
              if (session === "fresh") await client.initialize();
              await auth.invoke(client, auth.mock.profiles.noAuth, "anonymous");
            } catch (error) {
              // Only availability refusals are soft. Identity, routing, protocol,
              // and header failures remain hard; do not attach raw error payloads.
              if (
                !(error instanceof Error) ||
                !/^(tools\/list|initialize) failed \(HTTP (401|403)\)$/.test(
                  error.message,
                )
              )
                throw error;
              refused = true;
              outcome = error.message;
              expect
                .soft(
                  false,
                  `G8 availability: ${stage}: ${session}: ${outcome}`,
                )
                .toBe(true);
            }
            await test.info().attach("unsigned-session-outcome", {
              body: JSON.stringify({ stage, session, outcome }),
              contentType: "application/json",
            });
          },
          { timeout: 75_000 },
        );
      }
      const calls = auth.mock.events
        .slice(offset)
        .filter((event) => event.method === "tools/call");
      for (const call of calls) {
        expect(call).toMatchObject({
          resource: auth.mock.profiles.noAuth,
          principal: "anonymous",
          outcome: "authorized",
        });
        expect(call.headerNames).not.toContain("authorization");
        expect(call.headerNames).not.toContain("x-fixture-key");
        expect(call.headerNames).not.toContain("x-fixture-user");
      }
      if (refused)
        expect
          .soft(calls.length, "G8: both unsigned sessions must reach upstream")
          .toBeGreaterThanOrEqual(2);
      else expect(calls.length).toBeGreaterThanOrEqual(2);
    }
    // Measure configuration-time availability even if B's positive control
    // subsequently fails. This is not proof of post-login header isolation.
    await measureAnonymous("unsigned-after-config-before-B-login");
    const personal = auth.client(signed.endpoint);
    await auth.authorize(personal);
    test.info().annotations.push({
      type: "auth-stage",
      description: "signed-B-logged-in",
    });
    await auth.graph();
    await auth.invoke(personal, auth.mock.profiles.oauthAlpha, "alpha-user");
    await measureAnonymous("unsigned-after-B-login");
    await auth.invoke(personal, auth.mock.profiles.oauthAlpha, "alpha-user");
  });
}
