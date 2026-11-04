import { test, expect } from "./fixtures.ts";
import { FixtureMcpClient } from "./client.ts";
import {
  createWorkloadAgent,
  exchangeWorkload,
  startWorkloadIssuer,
} from "./workload-issuer.ts";

test.use({ trace: "off", screenshot: "off", video: "off" });

test("G10 headless: personal-only offer requires setup; UI reconnects it and the same workload succeeds without a choice", async ({
  auth,
}) => {
  test.setTimeout(240_000);
  const issuer = await startWorkloadIssuer();
  const { dashboard, mock } = auth;
  try {
    const server = await dashboard.remote(
      "fixture-headless-personal",
      mock.profiles.multiMethod,
      "user",
    );
    // The workload resolves its assigned owner's personal connection. Establish
    // it through real consent so the wizard can create the assignment.
    const owner = auth.client(server.endpoint);
    await auth.authorize(owner);
    await auth.invoke(owner, mock.profiles.multiMethod, "alpha-user");
    await createWorkloadAgent(auth, issuer, server);
    const metadata = await auth.client(server.endpoint).discover();
    // Protocol-only mock control: revoke at the upstream, not in application state.
    mock.revokeTokens();
    await expect(owner.listTools()).rejects.toThrow(/HTTP (401|403)/);
    await test.step("Personal-only headless grant returns setup_required without upstream execution", async () => {
      const offset = mock.events.length;
      const denied = await exchangeWorkload(issuer, metadata, true);
      expect(denied.status).toBe(400);
      expect(denied.error).toBe("setup_required");
      expect(denied.access_token === undefined).toBe(true);
      expect(
        mock.events
          .slice(offset)
          .filter(
            (event) =>
              event.method === "tools/call" && event.outcome === "authorized",
          ),
      ).toHaveLength(0);
    });
    await test.step("Owner reconnects the personal credential through actual browser consent", async () => {
      await auth.authorize(owner);
      await auth.invoke(owner, mock.profiles.multiMethod, "alpha-user");
    });
    await test.step("No explicit choice: the same workload receives and executes with the sole ready offer", async () => {
      const token = await exchangeWorkload(issuer, metadata, true);
      expect(token.status).toBe(200);
      expect(typeof token.access_token).toBe("string");
      const client = new FixtureMcpClient({
        endpoint: server.endpoint,
        bearerToken: token.access_token!,
        allowInsecureLoopback: true,
        trustedOrigins: [new URL(server.endpoint).origin],
      });
      try {
        await client.initialize();
        await auth.invoke(client, mock.profiles.multiMethod, "alpha-user");
      } finally {
        await client.close();
      }
    });
  } finally {
    await issuer.close();
  }
});
