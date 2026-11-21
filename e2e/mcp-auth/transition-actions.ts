import { randomUUID } from "node:crypto";
import type { Page } from "@playwright/test";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import type { FixtureMcpClient } from "./client.ts";
import { gatewayEchoes } from "./gateway.ts";

export function phase(id: string, stage: string) {
  test
    .info()
    .annotations.push({ type: "auth-stage", description: `${id}:${stage}` });
}

/** Stop before approval, after real login/upstream handbacks and tool capture. */
export async function reviewConsent(page: Page, url: string, id: string) {
  phase(id, "capture-consent-without-approval");
  await page.goto(url);
  for (let step = 0; step < 20; step++) {
    const give = page.getByRole("button", { name: "Give access", exact: true });
    const actions = [
      page.getByRole("button", { name: "Log in", exact: true }),
      page.getByRole("button", { name: "Approve mock access", exact: true }),
      page.getByRole("button", {
        name: "Give access to all tools",
        exact: true,
      }),
      page
        .getByRole("link", { name: /^(?:Connect|Reconnect)(?:\s|$)/ })
        .first(),
      page
        .getByRole("button", { name: /^(?:Connect|Reconnect)(?:\s|$)/ })
        .first(),
    ];
    await expect
      .poll(
        async () => {
          if ((await give.isVisible()) && (await give.isEnabled())) return true;
          for (const action of actions)
            if (await action.isVisible()) return true;
          return false;
        },
        { timeout: 15_000, message: `${id}: actionable review must render` },
      )
      .toBe(true);
    if ((await give.isVisible()) && (await give.isEnabled())) {
      await expect(
        page.getByText(
          "Tools could not be captured. This gateway cannot be approved. Try again later.",
          { exact: true },
        ),
      ).not.toBeVisible();
      await expect(page.locator('input[name="tool_inventory_id"]')).toHaveValue(
        /\S+/,
      );
      return;
    }
    for (const action of actions) {
      if (await action.isVisible()) {
        await action.click();
        break;
      }
    }
  }
  throw new Error(`${id}: consent did not reach review in 20 UI transitions`);
}

export function serverSlug(server: { serverURL: string }) {
  return new URL(server.serverURL).pathname.split("/").at(-1)!;
}

export async function assertGatewayCalls(
  auth: AuthScenario,
  client: FixtureMcpClient,
  members: { serverURL: string; resource: string; principal: string }[],
  id: string,
) {
  phase(id, "gateway-discovery-and-resource-routing");
  const echoes = await gatewayEchoes(client, members.map(serverSlug));
  for (const [index, member] of members.entries()) {
    const offset = auth.mock.events.length;
    const marker = randomUUID();
    const result = await echoes[index]!.call({ marker });
    expect(result, `${id}: member call succeeds`).toMatchObject({
      isError: false,
    });
    expect(JSON.stringify(result)).toContain(marker);
    const calls = auth.mock.events
      .slice(offset)
      .filter((event) => event.method === "tools/call");
    expect(calls.length, `${id}: upstream execution evidence`).toBeGreaterThan(
      0,
    );
    for (const call of calls)
      expect(call).toMatchObject({
        resource: member.resource,
        principal: member.principal,
        outcome: "authorized",
      });
  }
}

export async function connectionStatuses(client: FixtureMcpClient) {
  const result = await client.callTool("connection_status");
  // MCP successful results may omit isError; only true denotes tool failure.
  expect(result).not.toMatchObject({ isError: true });
  const structured = (
    result as {
      structuredContent?: {
        servers?: { slug: string; name: string; status: string }[];
      };
    }
  ).structuredContent;
  expect(structured?.servers).toBeInstanceOf(Array);
  return structured!.servers!;
}

/** A new, explicitly reviewed grant, never a retry of failed execution. */
export async function authorizeReviewed(
  auth: AuthScenario,
  client: FixtureMcpClient,
  id: string,
  mode: "live" | "frozen" = "frozen",
) {
  const flow = await client.beginAuthorization();
  try {
    await reviewConsent(auth.page, flow.authorizationUrl, id);
    const radio = auth.page.locator(
      `input[name="consent-tool-access-mode"][value="${mode === "live" ? "all" : "cached"}"]`,
    );
    await radio.focus();
    await radio.press("Space");
    await expect(radio).toBeChecked();
    await auth.page
      .getByRole("button", { name: "Give access", exact: true })
      .click();
    await flow.complete();
    auth.trackGrant(client);
    await client.initialize();
  } finally {
    await flow.cancel();
  }
}
