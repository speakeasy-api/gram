import { randomUUID } from "node:crypto";
import { test, expect } from "./fixtures.ts";
import { addStaticOffer } from "./credential-actions.ts";
import { ProfileActions } from "./profile-actions.ts";

// This covers the administrator's issuance-profile surface, not assistant turns
// or headless token issuance. Existing sessions deliberately keep their choice.
// Multiple offers alone do not prove ambiguity: one eligible OAuth registration
// can select personal OAuth automatically. This test proves only saved overrides.
test.use({ trace: "off", screenshot: "off", video: "off" });

test("G27 administration: explicit app choices among mixed offers persist through removal and recovery", async ({
  auth,
}) => {
  test.setTimeout(240_000);
  test.info().annotations.push({
    type: "scenario",
    description:
      "G27: issuance-profile UI persistence; not assistant execution or headless issuance proof",
  });
  const { page, dashboard, mock } = auth;
  const profiles = new ProfileActions(page);
  const caller = `fixture-profile-app-${randomUUID().slice(0, 8)}`;
  const first = `fixture-profile-first-${randomUUID().slice(0, 8)}`;
  const second = `fixture-profile-second-${randomUUID().slice(0, 8)}`;

  const { server, personal } = await test.step(
    "UI provision and prove personal execution baseline",
    async () => {
      const server = await dashboard.remote(
        `fixture-profile-choice-${randomUUID().slice(0, 8)}`,
        mock.profiles.multiMethod,
        "user",
      );
      const personal = auth.client(server.endpoint, undefined, {
        clientName: caller,
      });
      await auth.authorize(personal);
      await auth.invoke(personal, mock.profiles.multiMethod, "alpha-user");
      return { server, personal };
    },
    { timeout: 90_000 },
  );

  await test.step(
    "UI add two alternatives without silently assigning an app choice",
    async () => {
      await page.goto(`${server.serverURL}/settings`);
      await addStaticOffer(page, first, "fixture-static-secret");
      await addStaticOffer(page, second, "fixture-static-alt-secret");
      await expect(
        page.getByText(
          "None set. A caller with one ready credential uses it; with several, its sign-in asks it to name one.",
          { exact: true },
        ),
      ).toBeVisible();
      await expect(profiles.row(caller)).toHaveCount(0);
    },
    { timeout: 45_000 },
  );

  await test.step(
    "Administrator chooses one offer and a fresh UI read retains it",
    async () => {
      await profiles.choose(caller, first);
      await page.reload();
      await expect(profiles.row(caller)).toContainText(first);
      await expect(profiles.row(caller)).not.toContainText(second);
      // An administrative default does not rewrite a session's frozen choice.
      await auth.invoke(personal, mock.profiles.multiMethod, "alpha-user");
    },
    { timeout: 30_000 },
  );

  await test.step(
    "Remove the choice and verify it stays absent after reload",
    async () => {
      await profiles.remove(caller);
      await page.reload();
      await expect(
        page.getByRole("button", { name: "Choose for an app", exact: true }),
      ).toBeVisible();
      await expect(profiles.row(caller)).toHaveCount(0);
      await auth.invoke(personal, mock.profiles.multiMethod, "alpha-user");
    },
    { timeout: 30_000 },
  );

  await test.step(
    "Recover with the other explicit choice without changing existing sessions",
    async () => {
      await profiles.choose(caller, second);
      await page.reload();
      await expect(profiles.row(caller)).toContainText(second);
      await expect(profiles.row(caller)).not.toContainText(first);
      await auth.invoke(personal, mock.profiles.multiMethod, "alpha-user");
    },
    { timeout: 30_000 },
  );
});
