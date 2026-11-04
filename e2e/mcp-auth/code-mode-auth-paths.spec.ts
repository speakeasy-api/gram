import { randomUUID } from "node:crypto";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import type { FixtureMcpClient } from "./client.ts";
import { GatewayActions } from "./gateway-actions.ts";
import { createOAuthRegistration } from "./auth-setup-actions.ts";
import { addPersonalOffer } from "./personal-offer-actions.ts";
import { authorizeReviewed, phase } from "./transition-actions.ts";

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

async function execute(client: FixtureMcpClient, code: string) {
  const result = record(await client.callTool("execute", { code }));
  expect(
    result.isError,
    "CODE_MODE_EXECUTION: requires an enabled gateway and healthy code-runtime; a runtime_unavailable refusal is an environment prerequisite, not an auth failure",
  ).not.toBe(true);
  const execution = record(result.structuredContent);
  expect(execution.error).toBeUndefined();
  return execution.value;
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

test("G9 Code Mode: approved no-auth member needs explicit review when sign-in is added", async ({
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
    "Review no-auth access and execute the upstream echo through Python",
    async () => {
      phase(id, "initial-code-execution");
      await authorizeReviewed(auth, client, id);
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
  await test.step(
    "Administrator adds a typed personal sign-in offer to the same resource",
    async () => {
      phase(id, "admin-adds-auth");
      auth.mock.setResourceAuth(resource, true);
      const registration = await createOAuthRegistration(auth);
      await auth.page.goto(`${member.serverURL}/settings`);
      await addPersonalOffer(auth.page, label(), registration.name);
    },
    { timeout: 90_000 },
  );
  await test.step(
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
        ),
      );
      expect(recovery).toMatchObject({
        status: "authorization_required",
        reason: "consent_required",
      });
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
    },
    { timeout: 90_000 },
  );
  await test.step(
    "Explicit new browser review resumes authenticated Code Mode execution",
    async () => {
      phase(id, "explicit-code-reconsent");
      await authorizeReviewed(auth, client, id);
      await echo(auth, client, path, resource, "alpha-user");
    },
    { timeout: 120_000 },
  );
});
