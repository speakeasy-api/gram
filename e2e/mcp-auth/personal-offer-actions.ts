import { test, expect, type Page } from "@playwright/test";

/** Add an administrator-owned personal offer referencing a UI-created registration. */
export async function addPersonalOffer(
  page: Page,
  name: string,
  registrationClientId: string,
  registrationRowId?: string,
) {
  test.info().annotations.push({
    type: "auth-stage",
    description: "ui-topology:add-exact-personal-registration",
  });
  await page
    .getByRole("button", { name: "Add credential", exact: true })
    .click();
  const sheet = page.getByRole("dialog", {
    name: "Add a credential",
    exact: true,
  });
  const kind = sheet.locator("#add-credential-remote_session");
  await kind.focus();
  await kind.press("Space");
  await expect(kind).toBeChecked();
  await sheet.getByLabel("Name", { exact: true }).fill(name);
  const signIn = sheet.getByRole("combobox", { name: "Sign-in", exact: true });
  await signIn.click();
  if (registrationRowId !== undefined) {
    expect(
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
        registrationRowId,
      ),
      "UI_REGISTRATION_BLOCKER: observed registration row must be a UUID",
    ).toBe(true);
    const registration = page
      .getByRole("option")
      .and(page.locator(`[data-value="${registrationRowId}"]`));
    await expect
      .poll(() => registration.count(), {
        message:
          "UI_REGISTRATION_BLOCKER: exact observed registration must be offered",
      })
      .toBe(1);
    const description = registration.locator(".text-muted-foreground");
    expect((await description.count()) === 1).toBe(true);
    const renderedDescription = await description.innerText();
    expect(renderedDescription.trim().length > 0).toBe(true);
    await registration.click();
    await expect
      .poll(
        async () => {
          const selected = signIn.locator("span.text-muted-foreground");
          return (
            (await selected.count()) === 1 &&
            (await selected.innerText()) === renderedDescription
          );
        },
        {
          message:
            "UI_REGISTRATION_BLOCKER: selected description must remain unchanged",
        },
      )
      .toBe(true);
  } else {
    // Several registrations can have the same issuer label. Their description
    // ends with the exact registration display name/client ID; use that identity,
    // not the issuer label or composite trigger text (which also includes a logo).
    const escaped = registrationClientId.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const registrationIdentity = new RegExp(`(?:^| · )${escaped}$`);
    const registration = page.getByRole("option").filter({
      has: page.locator(".text-muted-foreground").filter({
        hasText: registrationIdentity,
      }),
    });
    await expect(
      registration,
      "UI_REGISTRATION_BLOCKER: exact recorded DCR registration must be offered",
    ).toHaveCount(1);
    await registration.click();
    await expect(signIn.locator("span.text-muted-foreground")).toHaveText(
      registrationIdentity,
    );
  }
  await sheet
    .getByRole("button", { name: "Add credential", exact: true })
    .click();
  await expect(sheet).not.toBeVisible();
  await expect(
    page
      .locator("#upstream-credentials")
      .getByRole("button")
      .filter({ has: page.getByText(name, { exact: true }) }),
  ).toHaveCount(1);
  await expect(
    page
      .locator("#upstream-credentials")
      .getByRole("switch", { name: `Turn off ${name}`, exact: true }),
  ).toBeChecked();
}
