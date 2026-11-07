// Where the stack under test lives. `mise run test:rbac-e2e` loads these from
// the worktree's mise env; running Playwright by hand needs them exported.

function required(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(
      `${name} is not set. Run this suite through \`mise run test:rbac-e2e\`, ` +
        "which loads the worktree's mise env.",
    );
  }
  return value.replace(/\/$/, "");
}

export const env = {
  get serverURL(): string {
    return required("GRAM_SERVER_URL");
  },
  get siteURL(): string {
    return required("GRAM_SITE_URL");
  },
  get devIdpURL(): string {
    return required("GRAM_DEVIDP_EXTERNAL_URL");
  },
  // The project every persona is evaluated against. The local seed creates it.
  projectSlug: process.env["RBAC_E2E_PROJECT"] ?? "default",
};
