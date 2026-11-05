import { createHash } from "node:crypto";
import { test as base, expect } from "./fixtures.ts";
import { configureEMA, disableEMA } from "./ema-actions.ts";
import { startEMAUpstream, type EMATrust } from "./ema-upstream.ts";
import { prepareEMA } from "./ema-bootstrap.ts";

// Expected identity comes from the real signed-in profile, before any assertion.
const test = base.extend<{
  ema: Awaited<ReturnType<typeof startEMAUpstream>>;
  emaIdentity: EMATrust & { chainedServers: string[] };
}>({
  ema: async ({}, use, info) => {
    const ema = await startEMAUpstream();
    try {
      await use(ema);
    } finally {
      await info.attach("ema-upstream-journal", {
        body: JSON.stringify(ema.journal),
        contentType: "application/json",
      });
      await ema.close();
    }
  },
  emaIdentity: async ({ auth, ema }, use) => {
    const setup = await prepareEMA(auth);
    const chainedServers: string[] = [];
    try {
      ema.trust(setup.trust);
      await use({ ...setup.trust, chainedServers });
    } finally {
      try {
        for (const serverURL of chainedServers)
          await disableEMA(auth, serverURL);
      } finally {
        await setup.cleanup();
      }
    }
  },
});
test.use({ trace: "off", screenshot: "off", video: "off" });
test.describe.configure({ retries: 0 });

for (const personal of [false, true]) {
  test(`G22 ${personal ? "personal OAuth offer with EMA fallback" : "EMA identity chain"}: UI setup redeems a verified assertion and runs an identity-bound tool`, async ({
    auth,
    ema,
    emaIdentity,
  }) => {
    test.setTimeout(240_000);
    const { subject, issuer, chainedServers } = emaIdentity;
    const server = await configureEMA(auth, ema, (serverURL) =>
      chainedServers.push(serverURL),
    );
    const caller = auth.client(server.endpoint);
    await auth.authorize(caller, personal ? server.personalName : undefined);
    const tools = await caller.listTools();
    const identityTool = tools.tools.filter((tool) =>
      /(^|_)identity$/.test(tool.name),
    );
    expect(identityTool).toHaveLength(1);
    const result = await caller.callTool(identityTool[0]!.name);
    const alias = (value: string) =>
      createHash("sha256").update(value).digest("hex").slice(0, 16);
    const expected = {
      principal: alias(subject),
      issuer: alias(issuer),
      resource: ema.resource,
      scope: ema.scope,
    };
    expect(
      ema.journal.filter((event) => event.kind === "exchange"),
    ).toContainEqual({ kind: "exchange", ...expected });
    expect(ema.journal.filter((event) => event.kind === "tool")).toEqual([
      { kind: "tool", ...expected },
    ]);
    expect(JSON.stringify(result)).toContain(expected.principal);
    expect(ema.journal.filter((event) => event.kind === "oauth_code")).toEqual(
      [],
    );
    const graph = await auth.graph();
    if (personal) {
      const recorded = graph.servers.find(
        (value) => value.name === server.name,
      );
      expect(recorded).toBeDefined();
      const personalClients = recorded!.offers
        .filter(
          (offer) =>
            offer.type === "remote_session" && offer.source === "admin",
        )
        .map((offer) => offer.client);
      expect(
        recorded!.selections.some(
          (selection) =>
            selection.typedClient !== null &&
            personalClients.includes(selection.typedClient),
        ),
      ).toBe(true);
    }
    // The upstream revokes its own permission, not application state. This does
    // not claim to test an administrator's typed-client permission editor.
    const offset = ema.journal.length;
    ema.deny();
    const denied = await caller.callTool(identityTool[0]!.name).then(
      (value) => JSON.stringify(value),
      (error) => String(error),
    );
    expect(denied).toMatch(
      /isError|401|403|unauthorized|credential|authorization/i,
    );
    expect(
      ema.journal.slice(offset).filter((event) => event.kind === "tool"),
    ).toEqual([]);
  });
}
