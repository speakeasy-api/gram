export type LogoutClient = {
  auth: {
    logout: () => Promise<unknown>;
  };
};

/**
 * Invalidates the dashboard session and leaves the app.
 *
 * Await logout so cookies/storage Clear-Site-Data can run, then always
 * navigate. A reject (network, 5xx, SDK unwrap) must not strand the UI
 * on a dashboard page. This does not survive a hung fetch — Chromium
 * never settles a response that includes Clear-Site-Data: "cache", so
 * that directive must stay off the logout XHR.
 */
export async function logoutToLogin(client: LogoutClient): Promise<void> {
  try {
    await client.auth.logout();
  } catch {
    // Logout may have failed before the session was cleared. Still leave
    // so a dead or half-torn-down session is not left on the dashboard.
  }
  window.location.replace("/login");
}
