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
  const name = "fixture-assistant-personal";
  let serverSlug = "";
  await test.step(
    "UI attach remote MCP to the existing managed Project Assistant",
    async () => {
      const server = await dashboard.remote(
        name,
        mock.profiles.oauthAlpha,
        "user",
      );
      // The persisted route carries the generated slug, not the display name.
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
    },
    { timeout: 30_000 },
  );

  await test.step(
    "Real assistant turn requests OAuth; browser consent completes and tool executes",
    async () => {
      await assistant.openChat();
      const offset = mock.events.length;
      await assistant.send(serverSlug);
      await assistant.authorize();
      // OAuth completion resumes the suspended assistant turn; only a real
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

test("G27 assistant execution: ambiguous execution profile requires administrator choice and tool uses that identity", async ({
  auth,
}) => {
  AssistantAuthActions.requireModel();
  const assistant = new AssistantAuthActions(auth);
  const { page, dashboard, mock } = auth;
  const name = "fixture-assistant-ambiguous";
  let serverSlug = "";
  const first = "fixture-assistant-first";
  const second = "fixture-assistant-second";
  await test.step(
    "UI create two static alternatives and attach the server to the managed assistant",
    async () => {
      const server = await dashboard.remote(
        name,
        mock.profiles.multiMethod,
        "user",
      );
      serverSlug = new URL(server.serverURL).pathname.split("/").at(-1)!;
      await addStaticOffer(page, first, "fixture-static-secret");
      await addStaticOffer(page, second, "fixture-static-alt-secret");
      await assistant.attach(serverSlug);
    },
    { timeout: 90_000 },
  );

  await test.step(
    "Actual execution profile is ambiguous; administrator binds the second credential",
    async () => {
      await expect(assistant.profile(name)).toContainText("Not chosen");
      await expect(assistant.profile(name).locator("..")).toContainText(
        "No credential chosen yet. Choose one.",
      );
      await assistant.choose(name, second);
    },
    { timeout: 30_000 },
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
