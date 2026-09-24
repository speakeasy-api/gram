// @vitest-environment node
import { expect, test } from "vitest";
import { GramCore } from "../sdk/src/core";
import { HTTPClient } from "../sdk/src/lib/http";
import { adminCreateRegistryEntry } from "../sdk/src/funcs/adminCreateRegistryEntry";
import { adminSaveRegistryEntry } from "../sdk/src/funcs/adminSaveRegistryEntry";
import { adminGetRegistryEntry } from "../sdk/src/funcs/adminGetRegistryEntry";
import { adminListRegistryEntries } from "../sdk/src/funcs/adminListRegistryEntries";
import { adminSetRegistryEntryPublished } from "../sdk/src/funcs/adminSetRegistryEntryPublished";
import { adminRegistryEntryFromJSON } from "../sdk/src/models/components/adminregistryentry";
import { saveRegistryEntryRequestBodyToJSON } from "../sdk/src/models/components/saveregistryentryrequestbody";

const id = "00000000-0000-4000-8000-000000000001";
const token = "2026-09-21T23:59:59.123456789Z";

test("five generated operations exist and tokens/raw JSON round-trip losslessly", () => {
  for (const op of [
    adminCreateRegistryEntry,
    adminSaveRegistryEntry,
    adminGetRegistryEntry,
    adminListRegistryEntries,
    adminSetRegistryEntryPublished,
  ])
    expect(typeof op).toBe("function");
  for (const updatedAt of ["2026-09-21T12:00:00.123456Z", token]) {
    const raw = '{"extension":9007199254740993}';
    const decoded = adminRegistryEntryFromJSON(
      JSON.stringify({
        id,
        created_at: updatedAt,
        updated_at: updatedAt,
        published: true,
        issues: [],
        data_json: raw,
      }),
    );
    expect(decoded.ok).toBe(true);
    if (!decoded.ok) throw decoded.error;
    const encoded = JSON.parse(
      saveRegistryEntryRequestBodyToJSON(decoded.value),
    );
    expect(encoded.updated_at).toBe(updatedAt);
    expect(encoded.data_json).toBe(raw);
  }
});

test("generated Create/Save preserve synthetic JSON and concurrency tokens", async () => {
  for (const raw of [
    '{"server":{"name":"io.example/test","version":"1"},"extension":9007199254740993}',
    JSON.stringify({ extension: '"\\\n'.repeat(1024) }),
  ]) {
    const bodies: string[] = [];
    const client = new GramCore({
      serverURL: "https://admin.example.com",
      httpClient: new HTTPClient({
        fetcher: async (input) => {
          const req = input as Request;
          bodies.push(await req.text());
          return new Response(
            JSON.stringify({
              id,
              data_json: raw,
              created_at: token,
              updated_at: token,
              published: true,
              issues: [],
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        },
      }),
    });
    const created = await adminCreateRegistryEntry(client, { dataJson: raw });
    expect(created.ok).toBe(true);
    const saved = await adminSaveRegistryEntry(client, {
      dataJson: raw,
      id,
      updatedAt: token,
    });
    expect(saved.ok).toBe(true);
    expect(bodies).toHaveLength(2);
    for (const body of bodies) {
      expect(JSON.parse(body).data_json).toBe(raw);
      expect(Buffer.byteLength(body)).toBeLessThan(16 << 20);
    }
    expect(JSON.parse(bodies[1]!).updated_at).toBe(token);
  }
});
