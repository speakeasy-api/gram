import { getServerURL } from "@/lib/utils";

/**
 * Thin RPC helper for corpus endpoints not yet covered by the generated SDK.
 * Replace individual call sites with SDK hooks as they become available.
 */
export async function rpc<T>(
  method: string,
  body: Record<string, unknown>,
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };

  const projectSlug = getProjectSlugFromPath();
  if (projectSlug) {
    headers["Gram-Project"] = projectSlug;
  }

  const res = await fetch(`${getServerURL()}/rpc/${method}`, {
    method: "POST",
    headers,
    credentials: "include",
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`RPC ${method} failed: ${res.status}`);
  }
  return res.json() as Promise<T>;
}

function getProjectSlugFromPath(): string | null {
  if (typeof window === "undefined") {
    return null;
  }

  const parts = window.location.pathname.split("/").filter(Boolean);
  if (parts[1] !== "projects" || parts.length < 3) {
    return null;
  }

  return parts[2] ?? null;
}
