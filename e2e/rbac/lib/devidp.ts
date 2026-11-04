import type { APIRequestContext } from "@playwright/test";
import { env } from "./env.ts";

// Thin client for the local identity provider's management API. It is how the
// suite creates people and decides who the next login signs in as: the
// provider signs every login in as its "current user" without a prompt, so
// switching that user and logging in again is how one run gets a session per
// persona.

type DevIdpUser = { id: string; email: string };
type DevIdpOrg = { id: string; slug: string; workos_id?: string };

export class DevIdp {
  constructor(private readonly request: APIRequestContext) {}

  private async call<T>(method: string, body: unknown): Promise<T> {
    const res = await this.request.post(`${env.devIdpURL}/rpc/${method}`, {
      data: body,
    });
    if (!res.ok()) {
      throw new Error(
        `dev-idp ${method} failed: ${res.status()} ${await res.text()}`,
      );
    }
    const text = await res.text();
    return (text ? JSON.parse(text) : {}) as T;
  }

  async findUser(email: string): Promise<DevIdpUser | undefined> {
    let cursor = "";
    do {
      const page = await this.call<{
        items: DevIdpUser[];
        next_cursor?: string;
      }>("users.list", cursor ? { cursor } : {});
      const hit = page.items.find((u) => u.email === email);
      if (hit) return hit;
      cursor = page.next_cursor ?? "";
    } while (cursor);
    return undefined;
  }

  async ensureUser(email: string, displayName: string): Promise<DevIdpUser> {
    return (
      (await this.findUser(email)) ??
      (await this.call<DevIdpUser>("users.create", {
        email,
        display_name: displayName,
      }))
    );
  }

  async ensureOrg(slug: string, name: string): Promise<DevIdpOrg> {
    let cursor = "";
    do {
      const page = await this.call<{
        items: DevIdpOrg[];
        next_cursor?: string;
      }>("organizations.list", cursor ? { cursor } : {});
      const hit = page.items.find((o) => o.slug === slug);
      if (hit) return hit;
      cursor = page.next_cursor ?? "";
    } while (cursor);
    return this.call<DevIdpOrg>("organizations.create", { slug, name });
  }

  // Makes orgId the user's only organization, so a persona's organization
  // cannot drift between runs.
  async ensureOnlyMembership(userId: string, orgId: string): Promise<void> {
    const page = await this.call<{
      items: { id: string; organization_id: string }[];
    }>("memberships.list", { user_id: userId });
    for (const m of page.items.filter((m) => m.organization_id !== orgId)) {
      await this.call("memberships.delete", { id: m.id });
    }
    if (page.items.some((m) => m.organization_id === orgId)) return;
    await this.call("memberships.create", {
      user_id: userId,
      organization_id: orgId,
      role: "member",
    });
  }

  async setCurrentUser(userId: string): Promise<void> {
    await this.call("devIdp.setCurrentUser", {
      mode: "oauth2-1",
      user_id: userId,
    });
  }

  // Back to the provider's default user: the developer the local seed made an
  // organization admin.
  async clearCurrentUser(): Promise<void> {
    await this.call("devIdp.clearCurrentUser", { mode: "oauth2-1" });
  }
}
