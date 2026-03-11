import type { Context, Next } from "hono";

const SECRET_KEY = process.env.SPEAKEASY_SECRET_KEY || "test-secret";

export async function authMiddleware(c: Context, next: Next) {
  const path = new URL(c.req.url).pathname;

  // Skip auth for the login endpoint (user-facing)
  if (path.endsWith("/login")) {
    return next();
  }

  const providerKey = c.req.header("speakeasy-auth-provider-key");
  if (!providerKey || providerKey !== SECRET_KEY) {
    console.log(
      "[auth] REJECTED: invalid provider key for %s (got %s)",
      path,
      providerKey,
    );
    return c.json(
      { error: "Unauthorized: invalid or missing provider key" },
      401,
    );
  }

  return next();
}
