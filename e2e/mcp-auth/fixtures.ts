import { createHash, randomUUID } from "node:crypto";
import { test as base, expect, type Page } from "@playwright/test";
import { Dashboard, approveAuthorization } from "./dashboard.ts";
import { FixtureMcpClient } from "./client.ts";
import { startMockAuthServer } from "./mock-server.ts";
import { captureAuthGraph, type AuthGraph } from "./graph.ts";
import {
  authorizationFailure,
  privateConsentSnapshot,
  privateProtocolFailureRecorder,
} from "./authorization-diagnostics.ts";

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
  authorize(
    client: FixtureMcpClient,
    choice?: string,
    observeConsent?: () => Promise<void>,
    options?: { initialize?: boolean },
  ): Promise<void>;
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
    setPhase("login");
    let created = false;
    const auth: AuthScenario = {
      page,
      dashboard,
      mock,
      async graph(additionalProjectURLs = []) {
        const graph = await captureAuthGraph(
          [
            ...dashboard.ownedProjects.map((project) => project.url),
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
          onProtocolFailure: privateProtocolFailureRecorder(info),
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
      async authorize(client, choice, observeConsent, options) {
        setPhase("consent");
        const upstreamOffset = mock.events.length;
        const flow = await client.beginAuthorization();
        try {
          if (choice === undefined)
            await approveAuthorization(
              page,
              flow.authorizationUrl,
              observeConsent,
            );
          else {
            await page.goto(flow.authorizationUrl);
            const login = page.getByRole("button", {
              name: "Log in",
              exact: true,
            });
            const pickerReady = page.locator(
              '[data-upstream-auth-option], select[name^="auth_client."]',
            );
            const singletonInput = page.locator(
              'input[type="hidden"][name="auth_client"][form="consent-approve-form"]',
            );
            const typedRadios = page.locator(
              'input[type="radio"][name="auth_client"]',
            );
            await expect
              .poll(
                async () =>
                  (await login.isVisible()) ||
                  (await singletonInput.count()) > 0 ||
                  (await authorizationFailure(page)) !== null ||
                  (await pickerReady.count()) > 0 ||
                  (await page
                    .getByRole("button", { name: "Give access", exact: true })
                    .isVisible()),
                { message: "UI_CHOICE_BLOCKER: wait for real consent choices" },
              )
              .toBe(true);
            if (await login.isVisible()) await login.click();
            await expect
              .poll(
                async () =>
                  (await pickerReady.count()) > 0 ||
                  (await singletonInput.count()) > 0 ||
                  (await authorizationFailure(page)) !== null,
                { message: "UI_CHOICE_BLOCKER: wait for consent or error" },
              )
              .toBe(true);
            const consentFailure = await authorizationFailure(page);
            if (consentFailure !== null)
              throw new Error(
                `UI_CHOICE_BLOCKER: consent error ${JSON.stringify(consentFailure)}`,
              );
            let singletonValue: string | undefined;
            if (
              (await singletonInput.count()) === 1 &&
              (await typedRadios.count()) === 0 &&
              (await pickerReady.count()) === 0
            ) {
              // The static sole-choice template has no visible choice label.
              // Assert its identity against a fresh read-only fixture snapshot.
              const graph = await auth.graph();
              const candidates = [
                ...new Set(
                  graph.servers
                    .filter(
                      (server) =>
                        graph.projects.includes(server.project) &&
                        !server.disabled &&
                        !server.deleted,
                    )
                    .flatMap((server) => server.offers)
                    .filter(
                      (offer) =>
                        offer.name === choice &&
                        offer.enabled &&
                        !offer.deleted &&
                        !offer.bindingDeleted &&
                        offer.registrationDeleted !== true &&
                        offer.issuerDeleted !== true,
                    )
                    .map((offer) => offer.client),
                ),
              ];
              expect(
                candidates.length,
                "UI_CHOICE_BLOCKER: requested singleton must identify exactly one live bound typed client",
              ).toBe(1);
              const value = await singletonInput.inputValue();
              expect(
                /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(
                  value,
                ),
                "UI_CHOICE_BLOCKER: singleton must contain a canonical typed client UUID",
              ).toBe(true);
              const alias = `client-${createHash("md5")
                .update(value + graphAliasSalt)
                .digest("hex")
                .slice(0, 12)}`;
              expect(
                alias,
                "UI_CHOICE_BLOCKER: singleton must be the exact requested typed client",
              ).toBe(candidates[0]);
              singletonValue = value;
              info.annotations.push({
                type: "singleton-choice-verified",
                description: alias,
              });
            } else {
              await expect(
                pickerReady,
                `TYPED_OFFER_DISCOVERY_REGRESSION: consent must expose the attached alternatives before selecting ${choice}`,
              ).not.toHaveCount(0);
            }
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
            } else if (singletonValue === undefined) {
              await expect(
                radio,
                `UI_CHOICE_BLOCKER: expose exactly ${choice}`,
              ).toHaveCount(1);
              if (await radio.isEnabled()) await radio.check();
            }
            const directChoiceVerified = async () =>
              !gatewayChosen &&
              (singletonValue !== undefined
                ? (await singletonInput.count()) === 1 &&
                  (await typedRadios.count()) === 0 &&
                  (await pickerReady.count()) === 0 &&
                  (await singletonInput.inputValue()) === singletonValue
                : (await radio.count()) === 1 &&
                  (await radio.isVisible()) &&
                  (await radio.isChecked()));
            // Continue in-place: navigating back would discard the selection.
            for (let step = 0; step < 20; step++) {
              const give = page.getByRole("button", {
                name: "Give access",
                exact: true,
              });
              const allTools = page.getByRole("button", {
                name: "Give access to all tools",
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
                    ((await directChoiceVerified()) &&
                      (await allTools.isVisible()) &&
                      (await allTools.isEnabled())) ||
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
                  singletonValue !== undefined ||
                  ((await radio.isVisible()) && (await radio.isChecked()))) &&
                (await give.isVisible()) &&
                (await give.isEnabled())
              ) {
                if (singletonValue !== undefined) {
                  expect(
                    (await singletonInput.count()) === 1 &&
                      (await typedRadios.count()) === 0 &&
                      (await pickerReady.count()) === 0 &&
                      (await singletonInput.inputValue()) === singletonValue,
                    "UI_CHOICE_BLOCKER: verified singleton must remain unchanged before approval",
                  ).toBe(true);
                }
                if (!gatewayChosen) {
                  expect(
                    await directChoiceVerified(),
                    "UI_CHOICE_BLOCKER: exact typed choice must remain selected before approval",
                  ).toBe(true);
                }
                await give.click();
                break;
              }
              if (
                (await directChoiceVerified()) &&
                (await allTools.isVisible()) &&
                (await allTools.isEnabled())
              ) {
                // Explicitly approve all tools through the direct-server UI;
                // this does not change the selected upstream credential.
                await allTools.click();
                continue;
              }
              if (await approve.isVisible()) await approve.click();
              else if (await connect.isVisible()) await connect.click();
              else await connectButton.click();
            }
          }
          await flow.complete();
          grants.add(client);
        } catch (error) {
          // Correlate this authorization attempt, not earlier discovery/setup,
          // with the strict upstream journal. Its entries contain no secrets.
          await privateConsentSnapshot(page, info).catch(() => {});
          await info.attach("authorization-attempt-failure", {
            body: JSON.stringify({
              choiceExplicit: choice !== undefined,
              error: await authorizationFailure(page),
              upstream: mock.events.slice(upstreamOffset),
            }),
            contentType: "application/json",
          });
          await flow.cancel();
          throw error;
        }
        if (options?.initialize !== false) {
          setPhase("runtime-initialize");
          await client.initialize();
        }
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
      setPhase("ui-topology");
      await dashboard.enterProject();
      created = true;
      await use(auth);
    } finally {
      info.annotations.push({
        type: "auth-matrix",
        description: JSON.stringify({
          topology: info.title,
          provisioning: "dashboard-only",
          lastPhase: info.annotations
            .filter((a) => a.type === "auth-stage")
            .at(-1)?.description,
          status: info.status,
        }),
      });
      let graphFailure: unknown;
      if (created && dashboard.ownedProjects.length > 0) {
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
      // Evidence and grant/session closure precede dashboard-only fixture cleanup.
      // Keep scenario failures primary; report teardown failures separately.
      const scenarioFailed = info.status !== "passed";
      const cleanup = {
        grantFailures: revoked.filter((result) => result.status === "rejected")
          .length,
        sessionFailures: closed.filter((result) => result.status === "rejected")
          .length,
        projectDeleteFailures: 0,
        mockCloseFailed: false,
      };
      if (dashboard?.createdProjects.length > 0) {
        for (const project of [...dashboard.createdProjects].reverse()) {
          try {
            dashboard.projectURL = project.url;
            // removeProject verifies membership in this instance's ownership ledger.
            await dashboard.removeProject();
          } catch {
            cleanup.projectDeleteFailures += 1;
          }
        }
      }
      try {
        await mock?.close();
      } catch {
        cleanup.mockCloseFailed = true;
      }
      const cleanupFailed =
        cleanup.grantFailures > 0 ||
        cleanup.sessionFailures > 0 ||
        cleanup.projectDeleteFailures > 0 ||
        cleanup.mockCloseFailed;
      if (cleanupFailed) {
        try {
          await info.attach("fixture-cleanup-failure", {
            body: JSON.stringify(cleanup),
            contentType: "application/json",
          });
        } catch {
          // This fixed annotation cannot leak resource names or cleanup errors.
          info.annotations.push({
            type: "fixture-cleanup-failure",
            description: "Cleanup diagnostics attachment unavailable",
          });
        }
      }
      if (!scenarioFailed && graphFailure !== undefined) throw graphFailure;
      if (!scenarioFailed && cleanupFailed)
        throw new Error(
          "FIXTURE_CLEANUP_FAILURE: see separate cleanup diagnostics",
        );
    }
  },
});
