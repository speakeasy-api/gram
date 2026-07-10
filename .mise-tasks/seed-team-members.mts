#!/usr/bin/env -S node --import tsx

//MISE description="Seed a handful of fake org members locally so the project-card facepile renders with visible stacking"

import crypto from "node:crypto";

import { intro, log, outro } from "@clack/prompts";
import { GramCore } from "#gram/client/core.js";
import { authInfo } from "#gram/client/funcs/authInfo.js";
import { $ } from "zx";

const FAKE_MEMBERS: { name: string; email: string; pravatarId: number }[] = [
  { name: "Ava Martinez", email: "ava.martinez@example.com", pravatarId: 12 },
  { name: "Leo Tanaka", email: "leo.tanaka@example.com", pravatarId: 13 },
  { name: "Priya Shah", email: "priya.shah@example.com", pravatarId: 14 },
  { name: "Noah Becker", email: "noah.becker@example.com", pravatarId: 15 },
  { name: "Yuki Watanabe", email: "yuki.watanabe@example.com", pravatarId: 16 },
  { name: "Sofia Rossi", email: "sofia.rossi@example.com", pravatarId: 17 },
  { name: "Jamal Carter", email: "jamal.carter@example.com", pravatarId: 18 },
  { name: "Mei Chen", email: "mei.chen@example.com", pravatarId: 19 },
  { name: "Ravi Iyer", email: "ravi.iyer@example.com", pravatarId: 20 },
  { name: "Hannah Olsen", email: "hannah.olsen@example.com", pravatarId: 21 },
];

async function authenticateViaDevIDP(serverURL: string): Promise<string> {
  const loginRes = await fetch(`${serverURL}/rpc/auth.login`, {
    redirect: "manual",
  });
  const authorizeURL = loginRes.headers.get("location");
  if (!authorizeURL) throw new Error("auth.login did not return a redirect");
  const nonceCookie = loginRes.headers
    .getSetCookie()
    .find((c) => c.startsWith("gram_auth_nonce="));
  if (!nonceCookie) throw new Error("auth.login did not set gram_auth_nonce");
  const nonceCookieValue = nonceCookie.split(";")[0];
  const authorizeRes = await fetch(authorizeURL, { redirect: "manual" });
  const callbackLocation = authorizeRes.headers.get("location");
  if (!callbackLocation) {
    throw new Error("dev-idp authorize did not return a redirect");
  }
  const callbackRes = await fetch(callbackLocation, {
    redirect: "manual",
    headers: { cookie: nonceCookieValue },
  });
  const sessionToken = callbackRes.headers.get("gram-session");
  if (!sessionToken) {
    throw new Error(
      `auth.callback did not return a session (status=${callbackRes.status})`,
    );
  }
  return sessionToken;
}

function sqlString(value: string | null): string {
  if (value === null) return "NULL";
  return `'${value.replace(/'/g, "''")}'`;
}

async function psql(sql: string): Promise<void> {
  const dbUser = process.env.DB_USER ?? "gram";
  const dbName = process.env.DB_NAME ?? "gram";
  await $`docker compose exec -T gram-db psql -U ${dbUser} -d ${dbName} -v ON_ERROR_STOP=1 -c ${sql}`.quiet();
}

async function main(): Promise<void> {
  intro("Seeding fake team members for facepile preview...");
  let success = false;
  using _ = {
    [Symbol.dispose]() {
      outro(success ? "Done." : "Seeding failed.");
    },
  };

  const serverURL = process.env["GRAM_SERVER_URL"];
  if (!serverURL) {
    throw new Error(
      "GRAM_SERVER_URL is not set — run via `mise run seed-team-members`",
    );
  }

  const gram = new GramCore({ serverURL });
  const sessionId = await authenticateViaDevIDP(serverURL);
  const res = await authInfo(gram, undefined, {
    sessionHeaderGramSession: sessionId,
  });
  if (!res.ok) {
    throw new Error(`authInfo failed: ${JSON.stringify(res.error)}`);
  }
  const orgId = res.value.result.activeOrganizationId;
  if (!orgId) throw new Error("No active organization on session");
  log.info(`Active org: ${orgId}`);

  // INSERT users with a stable id and workos_id derived from email so the task
  // is idempotent — rerunning won't duplicate or collide.
  const valueRows = FAKE_MEMBERS.map((m) => {
    const userId = `usr_seed_${crypto
      .createHash("sha1")
      .update(m.email)
      .digest("hex")
      .slice(0, 16)}`;
    const workosId = `seed_workos_${crypto
      .createHash("sha1")
      .update(m.email)
      .digest("hex")
      .slice(0, 16)}`;
    const photo = `https://i.pravatar.cc/150?img=${m.pravatarId}`;
    return { userId, workosId, photo, ...m };
  });

  const usersValues = valueRows
    .map(
      (r) =>
        `(${sqlString(r.userId)}, ${sqlString(r.email)}, ${sqlString(r.name)}, ${sqlString(r.photo)}, ${sqlString(r.workosId)})`,
    )
    .join(",\n");

  await psql(
    `INSERT INTO users (id, email, display_name, photo_url, workos_id) VALUES\n${usersValues}\nON CONFLICT (email) DO UPDATE SET display_name = EXCLUDED.display_name, photo_url = EXCLUDED.photo_url, workos_id = EXCLUDED.workos_id;`,
  );
  log.info(`Upserted ${valueRows.length} users`);

  const ourValues = valueRows
    .map(
      (r) =>
        `(${sqlString(orgId)}, ${sqlString(r.userId)}, ${sqlString(r.workosId)}, ${sqlString(`seed_mem_${r.userId}`)})`,
    )
    .join(",\n");

  await psql(
    `INSERT INTO organization_user_relationships (organization_id, user_id, workos_user_id, workos_membership_id) VALUES\n${ourValues}\nON CONFLICT (organization_id, user_id) DO NOTHING;`,
  );
  log.info(
    `Linked ${valueRows.length} members to org ${orgId}. Refresh the dashboard.`,
  );
  success = true;
}

await main();
