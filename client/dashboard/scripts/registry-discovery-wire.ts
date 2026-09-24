import assert from "node:assert/strict";
import { ServiceError } from "../src/sdk/src/models/errors/serviceerror.ts";
import { RegistryDiscoverydiscoverServersResponseBody } from "../src/sdk/src/models/errors/registrydiscoverydiscoverserversresponsebody.ts";
import { RegistryDiscovery } from "../src/sdk/src/sdk/registrydiscovery.ts";

const client = new RegistryDiscovery({
  serverURL: process.env.REGISTRY_TEST_URL!,
});
const security = {
  option2: {
    apikeyHeaderGramKey: process.env.REGISTRY_TEST_KEY!,
    projectSlugHeaderGramProject: process.env.REGISTRY_TEST_PROJECT!,
  },
};

const first = await client.discoverServers(security, {
  limit: 1,
  search: "io.example/sdk",
});
assert.equal(first.metadata.count, 1);
assert.ok(first.metadata.nextCursor);

const second = await client.discoverServers(security, {
  limit: 1,
  search: "io.example/sdk",
  cursor: first.metadata.nextCursor,
});
assert.equal(second.metadata.count, 1);
assert.notEqual(first.servers[0].server.name, second.servers[0].server.name);
assert.equal(second.metadata.nextCursor, undefined);

const serverName = "io.example/sdk-a";
const record = await client.discoverVersion(security, {
  serverName,
  version: "v/1+2",
});
assert.equal(record.server.name, serverName);
assert.equal(record.server.extension.nested, "retained");
assert.equal(record._meta.extension, "retained");

const versions = await client.discoverVersions(security, { serverName });
assert.equal(versions.metadata.count, 1);
assert.equal(versions.servers[0].server.version, "v/1+2");

await assert.rejects(
  client.discoverVersion(security, { serverName, version: "old" }),
  (error: unknown) => {
    assert.ok(error instanceof RegistryDiscoverydiscoverServersResponseBody);
    assert.equal(error.statusCode, 404);
    assert.equal(error.error, "registry entry not found");
    return true;
  },
);

for (const [credentials, status, name] of [
  [
    { option2: { ...security.option2, apikeyHeaderGramKey: "" } },
    401,
    "unauthorized",
  ],
  [
    { option2: { ...security.option2, apikeyHeaderGramKey: "invalid" } },
    401,
    "unauthorized",
  ],
  [
    {
      option2: {
        ...security.option2,
        apikeyHeaderGramKey: process.env.REGISTRY_TEST_INSUFFICIENT_KEY!,
      },
    },
    403,
    "forbidden",
  ],
  [
    {
      option2: {
        ...security.option2,
        projectSlugHeaderGramProject: process.env.REGISTRY_TEST_OTHER_PROJECT!,
      },
    },
    403,
    "forbidden",
  ],
] as const) {
  await assert.rejects(
    client.discoverServers(credentials),
    (error: unknown) => {
      assert.ok(error instanceof ServiceError);
      assert.equal(error.statusCode, status);
      assert.equal(error.headers.get("goa-error"), name);
      assert.equal(error.data$.name, name);
      assert.equal(error.fault, false);
      assert.equal(error.temporary, false);
      assert.equal(error.timeout, false);
      assert.ok(error.id.length > 0);
      assert.ok(error.message.length > 0);
      assert.deepEqual(JSON.parse(error.body), {
        name,
        id: error.id,
        message: error.message,
        fault: false,
        temporary: false,
        timeout: false,
      });
      return true;
    },
  );
}

await assert.rejects(
  client.discoverVersions(security, { serverName, updatedSince: "" }),
  (error: unknown) => {
    assert.ok(error instanceof RegistryDiscoverydiscoverServersResponseBody);
    assert.equal(error.statusCode, 400);
    assert.match(error.error, /updated_since/);
    return true;
  },
);

for (const [request, expectedError] of [
  [{ limit: 101 }, "invalid discovery request"],
  [{ search: "x".repeat(1025) }, "invalid registry list options"],
] as const) {
  await assert.rejects(
    client.discoverServers(security, request),
    (error: unknown) => {
      assert.ok(error instanceof RegistryDiscoverydiscoverServersResponseBody);
      assert.equal(error.statusCode, 400);
      assert.equal(error.error, expectedError);
      assert.deepEqual(JSON.parse(error.body), { error: expectedError });
      return true;
    },
  );
}

console.log(
  "Generated SDK: all three routes, encoded segments, pagination, auth and standard errors passed",
);
