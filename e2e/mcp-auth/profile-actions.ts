import type { Page } from "@playwright/test";
import { expect } from "./fixtures.ts";

/** Visible, administrator-owned issuance choices; never provisions credentials. */
export class ProfileActions {
  constructor(private readonly page: Page) {}

  row(caller: string) {
    return this.page.getByRole("listitem").filter({
      has: this.page.getByRole("button", {
        name: `Remove the choice for ${caller}`,
        exact: true,
      }),
    });
  }

  async choose(caller: string, credential: string) {
    const { page } = this;
    await page
      .getByRole("button", { name: "Choose for an app", exact: true })
      .click();
    const dialog = page.getByRole("dialog", {
      name: "Choose the credential an app uses",
      exact: true,
    });
    await dialog.getByRole("combobox", { name: "App", exact: true }).click();
    await page.getByRole("option", { name: caller, exact: true }).click();
    await dialog
      .getByRole("combobox", { name: "Credential", exact: true })
      .click();
    const option = page.getByRole("option").filter({ hasText: credential });
    await expect(option).toHaveCount(1);
    // Match the full name prefix, not an issuer-looking or first available option.
    await expect(option).toHaveText(
      new RegExp(`^${escapeRegex(credential)} · `),
    );
    await option.click();
    await dialog.getByRole("button", { name: "Save", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(this.row(caller)).toContainText(credential);
  }

  async remove(caller: string) {
    await this.row(caller)
      .getByRole("button", {
        name: `Remove the choice for ${caller}`,
        exact: true,
      })
      .click();
    await expect(this.row(caller)).toHaveCount(0);
  }
}

function escapeRegex(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
