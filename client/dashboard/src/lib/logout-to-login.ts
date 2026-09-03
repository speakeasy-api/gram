export type LogoutClient = {
  auth: {
    logout: () => Promise<unknown>;
  };
};

/**
 * Invalidates the dashboard session and leaves the app.
 *
 * The logout response can reject after the session is already gone
 * (Chromium aborts a fetch once Clear-Site-Data runs). Navigation must
 * not depend on that promise settling successfully.
 */
export async function logoutToLogin(client: LogoutClient): Promise<void> {
  try {
    await client.auth.logout();
  } catch {
    // Session teardown already happened server-side; still leave the page.
  }
  window.location.replace("/login");
}
