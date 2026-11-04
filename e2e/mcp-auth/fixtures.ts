import { randomUUID } from "node:crypto";
import { test as base, expect, type Page } from "@playwright/test";
import { Dashboard, approveAuthorization } from "./dashboard.ts";
import { FixtureMcpClient } from "./client.ts";
import { startMockAuthServer } from "./mock-server.ts";
import { captureAuthGraph, type AuthGraph } from "./graph.ts";

export { expect };
export type AuthScenario = {
  page: Page;
  dashboard: Dashboard;
  mock: Awaited<ReturnType<typeof startMockAuthServer>>;
  client(
    endpoint: string,
    headers?: Record<string, string>,
    options?: { clientName?: string },
  ): FixtureMcpClient;
  graph(additionalProjectURLs?: string[]): Promise<AuthGraph>;
  authorize(client: FixtureMcpClient, choice?: string): Promise<void>;
  revoke(client: FixtureMcpClient): Promise<void>;
  trackGrant(client: FixtureMcpClient): void;
  invoke(
    client: FixtureMcpClient,
    resource: string,
    principal?: string,
  ): Promise<void>;
};

type MockOptions = NonNullable<Parameters<typeof startMockAuthServer>[0]>;
export const test = base.extend<{
  auth: AuthScenario;
  mockOptions: MockOptions;
}>({
  mockOptions: [{}, { option: true }],
  auth: async ({ page, mockOptions }, use, info) => {
    const mock = await startMockAuthServer({
      accessTokenLifetimeSeconds: info.title.includes("expiry") ? 2 : 3600,
      ...mockOptions,
    });
    const dashboard = new Dashboard(page);
    const clients: FixtureMcpClient[] = [];
    const grants = new Set<FixtureMcpClient>();
    const graphAliasSalt = randomUUID().replaceAll("-", "");
    const setPhase = (phase: string) =>
      info.annotations.push({ type: "auth-stage", description: phase });
    setPhase("persisted-enforcement");
    let created = false;
    let enforcementVerified = false;
    const auth: AuthScenario = {
      page,
      dashboard,
      mock,
      async graph(additionalProjectURLs = []) {
        const graph = await captureAuthGraph(
          [
            ...dashboard.createdProjects.map((project) => project.url),
            ...additionalProjectURLs,
          ],
          graphAliasSalt,
        );
        await info.attach("fixture-auth-graph", {
          body: JSON.stringify(graph),
          contentType: "application/json",
        });
        return graph;
      },
      client(endpoint, headers, options) {
        const client = new FixtureMcpClient({
          endpoint,
          headers,
          clientName: options?.clientName,
          allowInsecureLoopback: true,
          trustedOrigins: [
            new URL(dashboard.site).origin,
            new URL(endpoint).origin,
            mock.origin,
          ],
        });
        clients.push(client);
        return client;
      },
      async authorize(client, choice) {
        setPhase("consent");
        const flow = await client.beginAuthorization();
        try {
          if (choice === undefined)
            await approveAuthorization(page, flow.authorizationUrl);
          else {
            await page.goto(flow.authorizationUrl);
            const login = page.getByRole("button", {
              name: "Log in",
              exact: true,
            });
            const pickerReady = page.locator(
              '[data-upstream-auth-option], select[name^="auth_client."]',
            );
            await expect
              .poll(
                async () =>
                  (await login.isVisible()) ||
                  (await pickerReady.count()) > 0 ||
                  (await page
                    .getByRole("button", { name: "Give access", exact: true })
                    .isVisible()),
                { message: "UI_CHOICE_BLOCKER: wait for real consent choices" },
              )
              .toBe(true);
            if (await login.isVisible()) await login.click();
            await expect(
              pickerReady,
              `TYPED_OFFER_DISCOVERY_REGRESSION: consent must expose the attached alternatives before selecting ${choice}`,
            ).not.toHaveCount(0);
            const radio = page
              .locator("[data-upstream-auth-option]")
              .filter({ has: page.getByText(choice, { exact: true }) })
              .getByRole("radio");
            let gatewayChosen = false;
            if (
              (await page.locator('select[name^="auth_client."]').count()) > 0
            ) {
              const details = page.locator("details[data-gateway-list]");
              if (
                (await details.count()) > 0 &&
                (await details.getAttribute("open")) === null
              )
                await details.locator("summary").click();
              const escaped = choice.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
              const matching = page
                .locator('select[name^="auth_client."]')
                .filter({
                  has: page.getByRole("option", {
                    name: new RegExp(`^${escaped} · `),
                  }),
                });
              const count = await matching.count();
              expect(
                count,
                `UI_CHOICE_BLOCKER: gateway must expose ${choice} on a member`,
              ).toBeGreaterThan(0);
              for (let index = 0; index < count; index++) {
                const picker = matching.nth(index);
                const option = picker.getByRole("option", {
                  name: new RegExp(`^${escaped} · `),
                });
                const value = await option.getAttribute("value");
                if (!value)
                  throw new Error(
                    "UI_CHOICE_BLOCKER: option must have its rendered value",
                  );
                await picker.selectOption(value);
                await expect(picker.locator("option:checked")).toContainText(
                  choice,
                );
              }
              gatewayChosen = true;
            } else {
              await expect(
                radio,
                `UI_CHOICE_BLOCKER: expose exactly ${choice}`,
              ).toHaveCount(1);
              if (await radio.isEnabled()) await radio.check();
            }
            // Continue in-place: navigating back would discard the selection.
            for (let step = 0; step < 20; step++) {
              const give = page.getByRole("button", {
                name: "Give access",
                exact: true,
              });
              const approve = page.getByRole("button", {
                name: "Approve mock access",
                exact: true,
              });
              const connect = page
                .getByRole("link", { name: /^(?:Connect|Reconnect)(?:\s|$)/ })
                .first();
              const connectButton = page
                .getByRole("button", { name: /^(?:Connect|Reconnect)(?:\s|$)/ })
                .first();
              await expect
                .poll(
                  async () =>
                    ((await give.isVisible()) && (await give.isEnabled())) ||
                    (await approve.isVisible()) ||
                    (await connect.isVisible()) ||
                    (await connectButton.isVisible()),
                  {
                    message:
                      "UI_CONSENT_BLOCKER: selected credential must be actionable",
                  },
                )
                .toBe(true);
              if ((await radio.isVisible()) && (await radio.isEnabled())) {
                await radio.check();
                await expect(radio).toBeChecked();
              }
              if (
                (gatewayChosen ||
                  ((await radio.isVisible()) && (await radio.isChecked()))) &&
                (await give.isVisible()) &&
                (await give.isEnabled())
              ) {
                await give.click();
                break;
              }
              if (await approve.isVisible()) await approve.click();
              else if (await connect.isVisible()) await connect.click();
              else await connectButton.click();
            }
          }
          await flow.complete();
          grants.add(client);
        } catch (error) {
          await flow.cancel();
          throw error;
        }
        setPhase("runtime-initialize");
        await client.initialize();
      },
      trackGrant(client) {
        grants.add(client);
      },
      async revoke(client) {
        await client.revoke();
        // Sibling teardown may delete its issuer after revocation. Do not send
        // a duplicate revocation to the removed issuer in base cleanup.
        grants.delete(client);
      },
      async invoke(client, resource, principal) {
        setPhase("runtime-invoke");
        const { tools } = await client.listTools();
        const echo = tools.find((tool) => tool.name.endsWith("fixture_echo"));
        expect(
          echo,
          "MCP_DISCOVERY_FAILURE: expected upstream echo",
        ).toBeDefined();
        const marker = randomUUID();
        const offset = mock.events.length;
        const result = await client.callTool(echo!.name, { marker });
        expect(result).toMatchObject({ isError: false });
        expect(JSON.stringify(result)).toContain(marker);
        const calls = mock.events
          .slice(offset)
          .filter((event) => event.method === "tools/call");
        expect(calls.length, "UPSTREAM_ROUTING_FAILURE").toBeGreaterThan(0);
        expect(
          calls.every(
            (event) =>
              event.resource === resource &&
              event.outcome === "authorized" &&
              (!principal || event.principal === principal),
          ),
          "UPSTREAM_IDENTITY_FAILURE",
        ).toBe(true);
      },
    };
    try {
      await dashboard.login();
      await dashboard.enforce();
      enforcementVerified = true;
      info.annotations.push({
        type: "auth-path",
        description:
          "Persisted typed-client enforcement verified through a fresh staff UI read before fixture creation",
      });
      setPhase("ui-topology");
      await dashboard.createProject(`auth-e2e-${randomUUID().slice(0, 8)}`);
      created = true;
      await use(auth);
    } finally {
      info.annotations.push({
        type: "auth-matrix",
        description: JSON.stringify({
          topology: info.title,
          provisioning: "dashboard-only",
          enforcement: enforcementVerified ? "persisted" : "not-verified",
          lastPhase: info.annotations
            .filter((a) => a.type === "auth-stage")
            .at(-1)?.description,
          status: info.status,
        }),
      });
      let graphFailure: unknown;
      if (created && dashboard.createdProjects.length > 0) {
        try {
          await auth.graph();
        } catch (error) {
          graphFailure = error;
          await info.attach("fixture-auth-graph-error", {
            body: "Local read-only topology evidence unavailable",
            contentType: "text/plain",
          });
        }
      }
      // Journals expose metadata, not request headers or token response bodies.
      await info.attach("fixture-journal", {
        body: JSON.stringify(mock.events, null, 2),
        contentType: "application/json",
      });
      await info.attach("protocol-journal", {
        body: JSON.stringify(
          clients.map((client) => client.transcript),
          null,
          2,
        ),
        contentType: "application/json",
      });
      const revoked = await Promise.allSettled(
        [...grants].map((client) => client.revoke()),
      );
      const closed = await Promise.allSettled(
        clients.map((client) => client.close()),
      );
      try {
        await dashboard.restoreEnforcement();
        if (
          created &&
          info.status === "passed" &&
          revoked.every((r) => r.status === "fulfilled") &&
          closed.every((r) => r.status === "fulfilled")
        )
          await dashboard.removeCreatedProjects();
      } finally {
        await mock.close();
      }
      if (info.status === "passed" && graphFailure !== undefined)
        throw graphFailure;
      expect(
        revoked.filter((r) => r.status === "rejected"),
        "PROTOCOL_CLEANUP_FAILURE: issued grants",
      ).toHaveLength(0);
      expect(
        closed.filter((r) => r.status === "rejected"),
        "PROTOCOL_CLEANUP_FAILURE: sessions",
      ).toHaveLength(0);
    }
  },
});
