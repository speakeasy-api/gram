import { randomUUID } from "node:crypto";
import { test, expect } from "./fixtures.ts";
import { AssistantAuthActions } from "./assistant-auth-actions.ts";
import { addStaticOffer } from "./credential-actions.ts";

// OAuth links carry bearer state. Keep browser artifacts off; fixture journals
// record sanitized upstream identity evidence instead of browser credentials.
test.use({ trace: "off", screenshot: "off", video: "off" });
test.setTimeout(360_000);

test("G16 assistant execution: personal OAuth choice binds a profile and later tool turns do not sign in again", async ({
  auth,
}) => {
  AssistantAuthActions.requireModel();
  const assistant = new AssistantAuthActions(auth);
  const { page, dashboard, mock } = auth;
  const name = `fixture-assistant-personal-${randomUUID().slice(0, 8)}`;
  let serverSlug = "";
  let serverURL = "";
  await test.step(
    "UI attach remote MCP to the existing managed Project Assistant",
    async () => {
      const server = await dashboard.remote(
        name,
        mock.profiles.oauthAlpha,
        "user",
      );
      // The persisted route carries the generated slug, not the display name.
      serverURL = server.serverURL;
      serverSlug = new URL(server.serverURL).pathname.split("/").at(-1)!;
      await assistant.attach(serverSlug);
    },
    { timeout: 60_000 },
  );

  await test.step(
    "UI explicitly bind the user-backed execution profile to the personal credential",
    async () => {
      const control = assistant.profile(name);
      await expect(control).toHaveCount(1);
      await control.click();
      const personal = page
        .getByRole("option")
        .filter({ hasText: " · Each person's own account" });
      await expect(personal).toHaveCount(1);
      const label = await personal.innerText();
      await personal.click();
      await expect(control).toHaveText(label);
      await page.reload();
      await assistant.identity();
      await expect(assistant.profile(name)).toHaveText(label);
      await assistant.grantCredentials(serverURL, name, [
        label.split(" · Each person's own account")[0]!,
      ]);
    },
    { timeout: 90_000 },
  );

  await test.step(
    "Real assistant turn requests OAuth; browser consent completes and tool executes",
    async () => {
      await assistant.openChat();
      const offset = mock.events.length;
      await assistant.send(serverSlug);
      await assistant.authorize(serverSlug);
      // Completion resumes the turn or retries after connection recovery; only a real
      // tools/call at the fixture establishes successful execution.
      await assistant.executedSince(
        offset,
        mock.profiles.oauthAlpha,
        "alpha-user",
      );
    },
    { timeout: 150_000 },
  );

  await test.step(
    "A second actual tool turn reuses the bound personal connection",
    async () => {
      const offset = mock.events.length;
      const authorizations = mock.events.filter(
        (event) => event.kind === "authorize",
      ).length;
      await assistant.send(serverSlug);
      await assistant.executedSince(
        offset,
        mock.profiles.oauthAlpha,
        "alpha-user",
      );
      expect(
        mock.events.filter((event) => event.kind === "authorize"),
      ).toHaveLength(authorizations);
    },
    { timeout: 100_000 },
  );
});

test("G27 assistant execution: mixed offers default to personal OAuth and administrator-selected static credentials determine tool identity", async ({
  auth,
}) => {
  AssistantAuthActions.requireModel();
  const assistant = new AssistantAuthActions(auth);
  const { page, dashboard, mock } = auth;
  const name = `fixture-assistant-mixed-${randomUUID().slice(0, 8)}`;
  let serverSlug = "";
  let serverURL = "";
  const first = `fixture-assistant-first-${randomUUID().slice(0, 8)}`;
  const second = `fixture-assistant-second-${randomUUID().slice(0, 8)}`;
  await test.step(
    "UI retain personal OAuth, add two static alternatives, and attach the server",
    async () => {
      const server = await dashboard.remote(
        name,
        mock.profiles.multiMethod,
        "user",
      );
      serverURL = server.serverURL;
      serverSlug = new URL(server.serverURL).pathname.split("/").at(-1)!;
      await addStaticOffer(page, first, "fixture-static-secret");
      await addStaticOffer(page, second, "fixture-static-alt-secret");
      await assistant.attach(serverSlug);
    },
    { timeout: 90_000 },
  );

  await test.step(
    "Mixed offers automatically choose personal OAuth; administrator explicitly binds the second static credential",
    async () => {
      const graph = await auth.graph();
      const servers = graph.servers.filter((server) => server.name === name);
      expect(servers).toHaveLength(1);
      const server = servers[0]!;
      expect(server.disabled).toBe(false);
      expect(server.deleted).toBe(false);
      const liveOffers = server.offers.filter(
        (offer) => offer.enabled && !offer.deleted && !offer.bindingDeleted,
      );
      expect(liveOffers).toHaveLength(3);
      expect(new Set(liveOffers.map((offer) => offer.client)).size).toBe(3);
      const staticOffers = liveOffers.filter(
        (offer) => offer.type === "static_credential",
      );
      expect(staticOffers).toHaveLength(2);
      expect(staticOffers.every((offer) => offer.registration === null)).toBe(
        true,
      );
      const personalOffers = liveOffers.filter(
        (offer) => offer.type === "remote_session",
      );
      expect(personalOffers).toHaveLength(1);
      expect(personalOffers[0]!.registration).not.toBeNull();
      expect(personalOffers[0]!.registrationDeleted).toBe(false);
      expect(personalOffers[0]!.issuerBridge).toBe("present");

      // executionprofiles/profiles.go decide(): one OAuth registration selects
      // its personal client for user-backed turns, even with static offers.
      // This mixed topology does not cover header-only credential ambiguity.
      const control = assistant.profile(name);
      await expect(control).toHaveCount(1);
      await control.click();
      const personal = page
        .getByRole("option")
        .filter({ hasText: " · Each person's own account" });
      await expect(personal).toHaveCount(1);
      const personalName = (await personal.innerText()).split(
        " · Each person's own account",
      )[0]!;
      await page.keyboard.press("Escape");
      await expect(control).toHaveText(`Automatic (${personalName})`);
      await assistant.choose(name, second);
      await assistant.grantCredentials(serverURL, name, [first, second]);
    },
    { timeout: 90_000 },
  );

  await test.step(
    "Real Project Assistant tool turn executes as the explicitly selected identity",
    async () => {
      await assistant.openChat();
      const offset = mock.events.length;
      await assistant.send(serverSlug);
      await assistant.executedSince(
        offset,
        mock.profiles.multiMethod,
        "static-alt-user",
      );
      const calls = mock.events
        .slice(offset)
        .filter(
          (event) =>
            event.method === "tools/call" &&
            event.resource === mock.profiles.multiMethod,
        );
      expect(
        calls.every((event) => event.principal === "static-alt-user"),
      ).toBe(true);
    },
    { timeout: 100_000 },
  );

  await test.step(
    "Changed execution-profile choice applies to a subsequent real tool turn",
    async () => {
      await assistant.settings();
      await assistant.choose(name, first);
      await assistant.openChat();
      const offset = mock.events.length;
      await assistant.send(serverSlug);
      await assistant.executedSince(
        offset,
        mock.profiles.multiMethod,
        "static-user",
      );
      const calls = mock.events
        .slice(offset)
        .filter(
          (event) =>
            event.method === "tools/call" &&
            event.resource === mock.profiles.multiMethod,
        );
      expect(calls.every((event) => event.principal === "static-user")).toBe(
        true,
      );
    },
    { timeout: 120_000 },
  );
});
