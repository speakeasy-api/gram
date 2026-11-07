import { test, expect, type AuthScenario } from "./fixtures.ts";
import type { EMATrust } from "./ema-upstream.ts";

const timeout = 60_000;
const userURN =
  /^user:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Read the human independently; change organization enablement only through UI. */
export async function prepareEMA(auth: AuthScenario): Promise<{
  trust: EMATrust;
  cleanup: () => Promise<void>;
}> {
  const { page, dashboard } = auth;
  const orgURL = dashboard.projectURL.split("/projects/")[0]!;
  const workspace = `${orgURL}/identity?tab=identity-providers&provider=speakeasy`;
  const status = page.locator("#status");
  const enabled = status.getByText("Enabled", { exact: true });
  const off = status.getByText("Off", { exact: true });
  let changedEnablement = false;
  let pinnedIssuer: string | undefined;

  const requireState = async () => {
    await expect(
      status,
      "EMA prerequisite: the Speakeasy workspace must expose Status. Organization admin access, enterprise issuer rollout, and deployment signer authority must be available; unavailable issuers are hidden by the dashboard.",
    ).toBeVisible({ timeout });
    await expect(
      enabled.or(off),
      "EMA prerequisite: only Enabled or Off is supported; do not repair Paused, Needs attention, or unavailable issuer states in this test.",
    ).toBeVisible({ timeout });
  };

  const cleanup = async () => {
    if (!changedEnablement) return;
    await page.goto(workspace);
    await requireState();
    if (await enabled.isVisible()) {
      await page
        .locator("#danger-zone")
        .getByRole("button", { name: "Disable", exact: true })
        .click();
      const dialog = page.getByRole("dialog", {
        name: "Disable Enterprise Managed Authorization?",
        exact: true,
      });
      await expect(dialog).toBeVisible({ timeout });
      await dialog
        .getByRole("button", { name: "Disable", exact: true })
        .click();
      await expect(off).toBeVisible({ timeout });
    }
    await page.reload();
    await expect(
      off,
      "EMA cleanup must persist Off after a fresh load",
    ).toBeVisible({ timeout });
    // Disabling retains the issuer and its keys, including an issuer first
    // created here. Cleanup restores Off, not the original absence of an issuer.
    if (pinnedIssuer) {
      await expect(page.locator("#issuer code")).toHaveText(pinnedIssuer);
    }
    changedEnablement = false;
  };

  try {
    await expect(page).toHaveURL(dashboard.projectURL);
    test.info().annotations.push({
      type: "auth-stage",
      description: "G22:read-signed-in-profile",
    });
    await page
      .getByRole("button", { name: "Account menu", exact: true })
      .click();
    const account = page
      .getByRole("menuitem")
      .and(page.locator('a[href*="/identities/user"]'))
      .filter({ has: page.locator("p + p") });
    await expect(
      account,
      "The signed-in account must link to exactly one human identity profile",
    ).toHaveCount(1);
    await expect(account.locator("p")).toHaveCount(2);
    const email = (await account.locator("p").nth(1).innerText()).trim();
    expect(email).toMatch(/^[^\s@]+@[^\s@]+$/);
    await account.click();
    const canonical = page.getByText(userURN);
    await expect(canonical).toHaveCount(1);
    await expect(canonical).toBeVisible({ timeout });
    const subject = (await canonical.innerText()).trim().slice("user:".length);
    // IdentityHeader renders the canonical URN two levels below its root.
    // Scope the email check there, not to other overview panels or the sidebar.
    const header = canonical.locator("../..");
    await expect(header.getByRole("heading", { level: 1 })).toHaveCount(1);
    await expect(header.getByText(email, { exact: true })).toHaveCount(1);
    await expect(header.getByText(email, { exact: true })).toBeVisible();

    test.info().annotations.push({
      type: "auth-stage",
      description: "G22:verify-human-email-domain",
    });
    await page.goto(`${orgURL}/identity?tab=sso`);
    const domains = page.getByRole("list", {
      name: "Verified domains",
      exact: true,
    });
    await expect(
      domains,
      "EMA requires the signed-in human's email domain to be verified in organization SSO",
    ).toBeVisible({ timeout });
    const domain = email.split("@")[1]!.toLowerCase();
    await expect
      .poll(
        async () =>
          (await domains.getByRole("listitem").allTextContents()).map((value) =>
            value.trim().toLowerCase(),
          ),
        {
          timeout,
          message:
            "The exact signed-in email domain must appear in Verified domains",
        },
      )
      .toContain(domain);

    test.info().annotations.push({
      type: "auth-stage",
      description: "G22:enable-organization-issuer",
    });
    await page.goto(workspace);
    await requireState();
    if (await off.isVisible()) {
      const enable = status.getByRole("button", {
        name: "Enable Enterprise Managed Authorization",
        exact: true,
      });
      await expect(
        enable,
        "EMA prerequisite: Off must offer Enable; verified domain, direct organization admin sign-in, rollout, and deployment signer configuration are required",
      ).toBeVisible({ timeout });
      await expect(enable).toBeEnabled({ timeout });
      // Arm cleanup before the click: a timeout may occur after the write.
      changedEnablement = true;
      await enable.click();
      await expect(
        enabled,
        "Enabling EMA must reach Enabled, not a paused or keyless state",
      ).toBeVisible({ timeout });
    }
    await page.reload();
    await requireState();
    await expect(enabled).toBeVisible({ timeout });
    const issuerCode = page.locator("#issuer code");
    await expect(issuerCode).toHaveCount(1);
    await expect(issuerCode).toBeVisible();
    const issuer = (await issuerCode.innerText()).trim();
    const url = new URL(issuer);
    expect(url.protocol).toBe("https:");
    expect(url.username).toBe("");
    expect(url.password).toBe("");
    expect(url.search).toBe("");
    expect(url.hash).toBe("");
    expect(url.pathname).toMatch(/^\/oauth\/idp\/org\/[^/]+$/);
    expect(issuer).toBe(`${url.origin}${url.pathname}`);
    // Pin the exact rendered organization issuer origin and path, never an
    // incoming assertion's iss or a project-specific issuer.
    pinnedIssuer = issuer;
    return {
      trust: { issuer, jwksURL: `${issuer}/jwks`, subject, email },
      cleanup,
    };
  } catch (error) {
    try {
      await cleanup();
    } catch (cleanupError) {
      throw new AggregateError(
        [error, cleanupError],
        "EMA bootstrap failed and UI enablement cleanup also failed",
      );
    }
    throw error;
  }
}
