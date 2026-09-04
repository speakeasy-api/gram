export type LogoutClient = {
  auth: {
    logout: () => Promise<unknown>;
  };
};

/** Upper bound so a hung logout fetch cannot trap the UI on the dashboard. */
export const LOGOUT_WAIT_MS = 8_000;

/**
 * Invalidates the dashboard session and leaves the app.
 *
 * Await logout so cookies/storage Clear-Site-Data can run, then always
 * navigate. A reject or stall (network, 5xx, SDK unwrap, hung fetch) must
 * not leave the UI on a dashboard page. Chromium never settles a response
 * that includes Clear-Site-Data: "cache", so that directive must stay off
 * the logout XHR — the timeout is a backstop, not a reason to put it back.
 */
export async function logoutToLogin(client: LogoutClient): Promise<void> {
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      client.auth.logout(),
      new Promise<never>((_, reject) => {
        timeout = setTimeout(() => {
          reject(new Error("logout timed out"));
        }, LOGOUT_WAIT_MS);
      }),
    ]);
  } catch {
    // Logout may have failed or stalled before the session was cleared.
    // Still leave so a dead or half-torn-down session is not left on
    // the dashboard.
  } finally {
    clearTimeout(timeout);
  }
  window.location.replace("/login");
}
