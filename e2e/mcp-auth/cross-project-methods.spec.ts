import { randomUUID } from "node:crypto";
import { test, expect, type AuthScenario } from "./fixtures.ts";
import { Dashboard } from "./dashboard.ts";
import {
  addStaticOffer,
  rotateStaticCredential,
} from "./credential-actions.ts";
import type { FixtureMcpClient } from "./client.ts";

test.describe.configure({ mode: "default" });

async function secondProject(auth: AuthScenario) {
  const dashboard = new Dashboard(auth.page);
  dashboard.organizationSlug = auth.dashboard.organizationSlug;
  await auth.page.goto(`${dashboard.site}/${dashboard.organizationSlug}`);
  await dashboard.enforce();
  await dashboard.createProject(`auth-e2e-${randomUUID().slice(0, 8)}`);
  return dashboard;
}

async function revokeKey(auth: AuthScenario, url: string) {
  await auth.page.goto(url);
  await auth.page.getByRole("button", { name: /^Revoke API key / }).click();
  const dialog = auth.page.getByRole("dialog", {
    name: "Revoke API key?",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "Confirm revoke", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
}

test("enforced cross-project: organization-scoped static client shares rotation across distinct issuers without merging personal identities", async ({
  auth,
}) => {
  const { page, dashboard, mock } = auth;
  const shared = `fixture-cross-project-static-${randomUUID().slice(0, 8)}`;
  const resource = mock.profiles.multiMethod;
  const a = await dashboard.remote("fixture-project-a", resource, "user", true);
  const personalAName = await dashboard.personalCredentialName();
  const personalA = auth.client(a.endpoint);
  await auth.authorize(personalA);
  await page.goto(`${a.serverURL}/settings`);
  await addStaticOffer(page, shared, "fixture-static-secret", true);
  const second = await secondProject(auth);
  const secondGrants: FixtureMcpClient[] = [];
  let passed = false;
  try {
    const b = await second.remote("fixture-project-b", resource, "user", true);
    const personalBName = await second.personalCredentialName();
    const personalB = auth.client(b.endpoint);
    // Discover both issuers over the actual resource challenges; a visually
    // different issuer label alone is insufficient to prove separation.
    expect((await personalA.discover()).authorizationServer).not.toBe(
      (await personalB.discover()).authorizationServer,
    );
    await auth.authorize(personalB);
    secondGrants.push(personalB);
    await page.goto(`${b.serverURL}/settings`);
    await second.attachExistingOffer(shared, a.name, b.name);
    const staticA = auth.client(a.endpoint);
    const staticB = auth.client(b.endpoint);
    await auth.authorize(staticA, shared);
    await auth.authorize(staticB, shared);
    secondGrants.push(staticB);
    await auth.invoke(staticA, resource, "static-user");
    await auth.invoke(staticB, resource, "static-user");
    // Both personal alternatives must remain explicitly selectable after the
    // cross-project graph exists, not merely survive as old sessions.
    for (const [server, choice, isSecond] of [
      [a, personalAName, false],
      [b, personalBName, true],
    ] as const) {
      const personal = auth.client(server.endpoint);
      await auth.authorize(personal, choice);
      if (isSecond) secondGrants.push(personal);
      await auth.invoke(personal, resource, "alpha-user");
    }
    await page.goto(`${a.serverURL}/settings`);
    await rotateStaticCredential(
      page,
      shared,
      "fixture-static-alt-secret",
      true,
    );
    for (const client of [staticA, staticB]) {
      await auth.invoke(client, resource, "static-alt-user");
      await client.refresh();
      await auth.invoke(client, resource, "static-alt-user");
    }
    await auth.invoke(personalA, resource, "alpha-user");
    await auth.invoke(personalB, resource, "alpha-user");
    passed = true;
  } finally {
    await auth.graph([second.projectURL]);
    // Revoke before deleting B's issuer and release base-cleanup ownership.
    for (const grant of secondGrants) await auth.revoke(grant);
    await second.restoreEnforcement();
    if (passed) await second.removeProject();
  }
});

test("enforced cross-project: project-scoped static is absent from other project picker and its issued agent cannot access that project", async ({
  auth,
}) => {
  const { page, dashboard, mock } = auth;
  const shared = `fixture-cross-project-static-${randomUUID().slice(0, 8)}`;
  const resource = mock.profiles.multiMethod;
  const a = await dashboard.remote(
    "fixture-private-project-a",
    resource,
    "user",
    true,
  );
  const personalA = auth.client(a.endpoint);
  await auth.authorize(personalA);
  await page.goto(`${a.serverURL}/settings`);
  await addStaticOffer(page, shared, "fixture-static-secret", false);
  const key = await dashboard.issueAgentKey(a.name, shared, resource);
  await dashboard.finishAgentSetup();
  await page.getByRole("link", { name: "Credentials", exact: true }).click();
  const credentialsURL = page.url();
  const agentA = auth.client(a.endpoint, { Authorization: `Bearer ${key}` });
  await agentA.initialize();
  await auth.invoke(agentA, resource, "static-user");
  const second = await secondProject(auth);
  const secondGrants: FixtureMcpClient[] = [];
  let passed = false;
  try {
    const b = await second.remote(
      "fixture-private-project-b",
      resource,
      "user",
      true,
    );
    const personalB = auth.client(b.endpoint);
    expect((await personalA.discover()).authorizationServer).not.toBe(
      (await personalB.discover()).authorizationServer,
    );
    await auth.authorize(personalB);
    secondGrants.push(personalB);
    const control = await second.remote(
      "fixture-local-control",
      mock.profiles.noAuth,
    );
    await addStaticOffer(
      page,
      "fixture-eligible-local-control",
      "fixture-static-secret",
    );
    expect(control.endpoint).not.toBe(b.endpoint);
    await page.goto(`${b.serverURL}/settings`);
    // An available local control prevents a missing/broken picker from being
    // mistaken for successful project-scope enforcement.
    await addStaticOffer(
      page,
      "fixture-project-b-local",
      "fixture-static-alt-secret",
    );
    await page
      .getByRole("button", { name: "Add credential", exact: true })
      .click();
    const sheet = page.getByRole("dialog");
    const existing = sheet.getByRole("radio", { name: /Existing credential/ });
    await existing.focus();
    await existing.press("Space");
    await expect(existing).toBeChecked();
    await sheet
      .getByRole("combobox", { name: "Credential", exact: true })
      .click();
    await expect(page.getByRole("listbox")).toBeVisible();
    await expect(
      page
        .getByRole("option")
        .filter({ hasText: "fixture-eligible-local-control" }),
    ).toHaveCount(1);
    await expect(
      page.getByRole("option").filter({ hasText: shared }),
    ).toHaveCount(0);
    await page.keyboard.press("Escape");
    await sheet.getByRole("button", { name: "Close", exact: true }).click();
    await expect(sheet).not.toBeVisible();
    const local = auth.client(b.endpoint);
    await auth.authorize(local, "fixture-project-b-local");
    secondGrants.push(local);
    await auth.invoke(local, resource, "static-alt-user");
    const crossed = auth.client(b.endpoint, { Authorization: `Bearer ${key}` });
    const offset = mock.events.length;
    test.info().annotations.push({
      type: "auth-stage",
      description: "runtime-cross-project-denial",
    });
    await expect(
      crossed.initialize(),
      "PROJECT_BOUNDARY_FAILURE: A's key must not authenticate on B",
    ).rejects.toThrow(/401|403/);
    expect(
      mock.events
        .slice(offset)
        .filter((event) => event.outcome === "authorized"),
    ).toHaveLength(0);
    await auth.invoke(agentA, resource, "static-user");
    await auth.invoke(personalA, resource, "alpha-user");
    await auth.invoke(personalB, resource, "alpha-user");
    passed = true;
  } finally {
    await auth.graph([second.projectURL]);
    await revokeKey(auth, credentialsURL);
    for (const grant of secondGrants) await auth.revoke(grant);
    await second.restoreEnforcement();
    if (passed) await second.removeProject();
  }
});
