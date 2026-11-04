import { test, expect } from "./fixtures.ts";
import { FixtureMcpClient } from "./client.ts";
import {
  createWorkloadAgent,
  exchangeWorkload,
  startWorkloadIssuer,
} from "./workload-issuer.ts";

test.use({ trace: "off", screenshot: "off", video: "off" });

test("G17 workload: one shared-AS token admits its assigned server and rejects a sibling", async ({
  auth,
}) => {
  test.setTimeout(240_000);
  const issuer = await startWorkloadIssuer();
  const { dashboard, mock } = auth;
  try {
    const allowed = await dashboard.remote(
      "fixture-workload-allowed",
      mock.profiles.noAuth,
    );
    const sibling = await dashboard.remote(
      "fixture-workload-sibling",
      mock.profiles.noAuth,
    );
    const allowedMetadata = await auth.client(allowed.endpoint).discover();
    const siblingMetadata = await auth.client(sibling.endpoint).discover();
    expect(allowedMetadata.authorizationServer).toBe(
      siblingMetadata.authorizationServer,
    );
    expect(allowedMetadata.tokenEndpoint).toBe(siblingMetadata.tokenEndpoint);
    // Owner setup is real browser consent; it does not substitute for the workload grant below.
    await auth.authorize(auth.client(allowed.endpoint));
    await createWorkloadAgent(auth, issuer, allowed);
    const token = await exchangeWorkload(issuer, allowedMetadata);
    expect(token.status).toBe(200);
    expect(typeof token.access_token).toBe("string");
    expect(token.refresh_token).toBeUndefined();
    expect(issuer.jwksReads).toBeGreaterThan(0);
    const clients = [allowed, sibling].map(
      (server) =>
        new FixtureMcpClient({
          endpoint: server.endpoint,
          bearerToken: token.access_token!,
          allowInsecureLoopback: true,
          trustedOrigins: [new URL(server.endpoint).origin],
        }),
    );
    const [right, wrong] = clients as [FixtureMcpClient, FixtureMcpClient];
    try {
      await test.step("Same workload token executes on the assigned server", async () => {
        await right.initialize();
        await auth.invoke(right, mock.profiles.noAuth, "anonymous");
      });
      await test.step("Sibling shares the AS but not the agent's server membership", async () => {
        const offset = mock.events.length;
        const error = await wrong.initialize().then(
          () => "unexpected success",
          (error: unknown) => String(error),
        );
        expect(error).toMatch(/401|403|unauthoriz|forbidden/i);
        expect(
          mock.events
            .slice(offset)
            .filter(
              (event) =>
                event.method === "tools/call" && event.outcome === "authorized",
            ),
        ).toHaveLength(0);
      });
      await test.step("The denied sibling request does not break the allowed workload session", async () => {
        await auth.invoke(right, mock.profiles.noAuth, "anonymous");
      });
    } finally {
      await Promise.all(clients.map((client) => client.close()));
    }
  } finally {
    await issuer.close();
  }
});
