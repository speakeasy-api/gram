#!/usr/bin/env -S node --import tsx

//MISE description="Seed fake organizations locally so the admin organizations list has paging, sorting, account types, disabled orgs and trial states to exercise"

import crypto from "node:crypto";

import { intro, log, outro } from "@clack/prompts";
import { $ } from "zx";

// Every seeded id and slug starts with this so real rows are never mistaken for
// fixtures, and so `id NOT LIKE 'seedorg%'` isolates real data.
const MARKER = "seedorg";

const ADJECTIVES = [
  "Northwind",
  "Larkspur",
  "Ironvale",
  "Sablewood",
  "Meridian",
  "Copperline",
  "Harborlight",
  "Quillstone",
];
const NOUNS = [
  "Analytics",
  "Logistics",
  "Robotics",
  "Systems",
  "Labs",
  "Networks",
  "Foundry",
  "Dynamics",
  "Ventures",
  "Instruments",
  "Collective",
  "Partners",
  "Optics",
  "Forge",
  "Provisions",
  "Signals",
];

// One org per adjective/noun pair keeps every slug unique.
const ORG_COUNT = ADJECTIVES.length * NOUNS.length;

const MEMBER_NAMES = [
  "Ada Kestrel",
  "Bo Marlow",
  "Cora Vance",
  "Dev Aturi",
  "Elin Sarto",
  "Fen Okabe",
  "Gwen Ashby",
  "Hugo Petrov",
  "Ida Renner",
  "Jonas Wilde",
  "Kira Lund",
  "Liam Osei",
];

const ACCOUNT_TYPES = ["free", "pro", "enterprise"] as const;

type Trial = {
  endsInDays: number;
  demotedDaysAgo?: number;
  convertedDaysAgo?: number;
};

/** Trial state is derived from dates, so each row is an offset from now plus
 *  the demoted/converted markers. Orgs past the end of this list never
 *  started a trial. */
const TRIALS: Trial[] = [
  // running; the first two straddle the 7-day "ending soon" threshold, so a
  // wrong threshold constant moves a row between the two states
  { endsInDays: 7.1 },
  { endsInDays: 9 },
  { endsInDays: 13 },
  { endsInDays: 18 },
  { endsInDays: 24 },
  { endsInDays: 31 },
  { endsInDays: 40 },
  { endsInDays: 55 },
  // ending soon
  { endsInDays: 6.9 },
  { endsInDays: 5.5 },
  { endsInDays: 4 },
  { endsInDays: 2 },
  { endsInDays: 0.5 },
  // expired
  { endsInDays: -1 },
  { endsInDays: -6 },
  { endsInDays: -15 },
  { endsInDays: -40 },
  { endsInDays: -120 },
  // demoted; the last is demoted despite a future end date
  { endsInDays: -30, demotedDaysAgo: 29 },
  { endsInDays: -75, demotedDaysAgo: 74 },
  { endsInDays: -200, demotedDaysAgo: 199 },
  { endsInDays: 12, demotedDaysAgo: 3 },
  // converted; the last is converted despite a future end date
  { endsInDays: -20, convertedDaysAgo: 25 },
  { endsInDays: -90, convertedDaysAgo: 95 },
  { endsInDays: -300, convertedDaysAgo: 305 },
  { endsInDays: 18, convertedDaysAgo: 4 },
];

function hash(value: string): string {
  return crypto.createHash("sha1").update(value).digest("hex").slice(0, 16);
}

function sqlString(value: string | null): string {
  if (value === null) return "NULL";
  return `'${value.replace(/'/g, "''")}'`;
}

function days(n: number): string {
  return `now() + interval '${n} days'`;
}

async function psql(sql: string): Promise<string> {
  const dbUser = process.env.DB_USER ?? "gram";
  const dbName = process.env.DB_NAME ?? "gram";
  // On stdin, not -c: the membership insert is larger than one argv entry.
  const out = await $({
    input: sql,
  })`docker compose exec -T gram-db psql -U ${dbUser} -d ${dbName} -v ON_ERROR_STOP=1 -At`.quiet();
  return out.stdout.trim();
}

async function main(): Promise<void> {
  intro("Seeding fake organizations for the admin organizations list...");
  let success = false;
  using _ = {
    [Symbol.dispose]() {
      outro(success ? "Done." : "Seeding failed.");
    },
  };

  const orgs = [...Array(ORG_COUNT).keys()].map((i) => {
    const name = `${ADJECTIVES[i % ADJECTIVES.length]} ${NOUNS[Math.floor(i / ADJECTIVES.length)]}`;
    const key = name.toLowerCase().replace(/ /g, "-");
    const trial = TRIALS[i] ?? null;
    const trialTier = i % 2 === 0 ? "pro" : "enterprise";
    // Age rank is a permutation of i (47 is coprime with ORG_COUNT), so trial
    // states interleave with the creation-date ordering instead of banding.
    const age = ((i * 47) % ORG_COUNT) * 9;
    return {
      id: `${MARKER}_${key}`,
      slug: `${MARKER}-${key}`,
      name,
      accountType: trial?.convertedDaysAgo
        ? trialTier
        : trial?.demotedDaysAgo
          ? "free"
          : ACCOUNT_TYPES[i % 3]!,
      // Stride coprime with the account-type cycle so every type has disabled
      // and enabled orgs.
      disabled: i % 7 === 4,
      workosId: i % 2 === 0 ? `${MARKER}_workos_${key}` : null,
      createdAt: days(-age), // 9 days apart: several years of history
      disabledAt: days(-1 - (i % 40)),
      // The list column still reads free_trial_ends_at, so set it per row
      // instead of letting the column default make every org look mid-trial.
      // Signup plus 14 days, clamped into the past: without the clamp an org
      // younger than 14 days gets a future date and reads as mid-trial.
      freeTrialEndsAt: days(trial ? trial.endsInDays : Math.min(14 - age, -1)),
      memberCount: 1 + (i % 12),
      trial,
      trialTier,
    };
  });

  const orgValues = orgs
    .map(
      (o) =>
        `(${sqlString(o.id)}, ${sqlString(o.name)}, ${sqlString(o.slug)}, ${o.createdAt}, ${o.createdAt}, ${sqlString(o.accountType)}, ${o.disabled ? o.disabledAt : "NULL"}, ${sqlString(o.workosId)}, ${o.createdAt}, ${o.freeTrialEndsAt})`,
    )
    .join(",\n");
  // Every id is marker-prefixed and deterministic, so a conflict can only be a
  // row this task wrote. Re-running re-anchors the dates to the current now().
  await psql(
    `INSERT INTO organization_metadata (id, name, slug, created_at, updated_at, gram_account_type, disabled_at, workos_id, free_trial_started_at, free_trial_ends_at) VALUES\n${orgValues}\nON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, slug = EXCLUDED.slug, created_at = EXCLUDED.created_at, updated_at = EXCLUDED.updated_at, gram_account_type = EXCLUDED.gram_account_type, disabled_at = EXCLUDED.disabled_at, workos_id = EXCLUDED.workos_id, free_trial_started_at = EXCLUDED.free_trial_started_at, free_trial_ends_at = EXCLUDED.free_trial_ends_at;`,
  );

  const trialValues = orgs
    .filter((o) => o.trial)
    .map(
      (o) =>
        `(${sqlString(o.id)}, ${sqlString(o.trialTier)}, ${days(o.trial!.endsInDays)}, ${o.trial!.convertedDaysAgo ? days(-o.trial!.convertedDaysAgo) : "NULL"}, ${o.trial!.demotedDaysAgo ? days(-o.trial!.demotedDaysAgo) : "NULL"})`,
    )
    .join(",\n");
  await psql(
    `INSERT INTO trials (organization_id, tier, ends_at, converted_at, demoted_at) VALUES\n${trialValues}\nON CONFLICT (organization_id) DO UPDATE SET tier = EXCLUDED.tier, ends_at = EXCLUDED.ends_at, converted_at = EXCLUDED.converted_at, demoted_at = EXCLUDED.demoted_at, updated_at = clock_timestamp();`,
  );

  // One shared pool of users; org i takes the first memberCount of them, so the
  // Members column varies without seeding hundreds of user rows.
  const members = MEMBER_NAMES.map((name) => {
    const email = `${MARKER}.${name.toLowerCase().replace(/ /g, ".")}@example.com`;
    return {
      id: `${MARKER}_usr_${hash(email)}`,
      email,
      name,
      workosId: `${MARKER}_workos_usr_${hash(email)}`,
    };
  });

  const userValues = members
    .map(
      (m) =>
        `(${sqlString(m.id)}, ${sqlString(m.email)}, ${sqlString(m.name)}, ${sqlString(m.workosId)})`,
    )
    .join(",\n");
  await psql(
    `INSERT INTO users (id, email, display_name, workos_id) VALUES\n${userValues}\nON CONFLICT DO NOTHING;`,
  );

  const relationshipValues = orgs
    .flatMap((o) =>
      members
        .slice(0, o.memberCount)
        .map(
          (m) =>
            `(${sqlString(o.id)}, ${sqlString(m.id)}, ${sqlString(m.workosId)}, ${sqlString(`${MARKER}_mem_${o.id}_${m.id}`)})`,
        ),
    )
    .join(",\n");
  await psql(
    `INSERT INTO organization_user_relationships (organization_id, user_id, workos_user_id, workos_membership_id) VALUES\n${relationshipValues}\nON CONFLICT (organization_id, user_id) DO UPDATE SET workos_user_id = EXCLUDED.workos_user_id, workos_membership_id = EXCLUDED.workos_membership_id, deleted_at = NULL, updated_at = clock_timestamp();`,
  );

  const summary = await psql(
    `SELECT count(*) || ' orgs, ' || count(*) FILTER (WHERE disabled_at IS NOT NULL) || ' disabled, ' || count(t.organization_id) || ' with a trial' FROM organization_metadata o LEFT JOIN trials t ON t.organization_id = o.id WHERE o.id LIKE '${MARKER}%';`,
  );
  log.info(`Seeded: ${summary}`);
  success = true;
}

await main();
