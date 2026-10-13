import assert from "node:assert/strict";
import { IncomingMessage } from "node:http";
import { Socket } from "node:net";
import { test } from "node:test";
import { fromAllowedPeer } from "./peers.mts";

function requestFrom(address: string | undefined): IncomingMessage {
  const req = new IncomingMessage(new Socket());
  Object.defineProperty(req.socket, "remoteAddress", { value: address });
  return req;
}

for (const address of [
  "127.0.0.1",
  "127.255.255.255",
  "::1",
  "::ffff:127.0.0.1",
  "::ffff:7f00:1",
  "100.64.0.0",
  "100.64.0.1",
  "100.127.255.255",
  "::ffff:100.64.0.1",
  "::ffff:6440:1",
  "fd7a:115c:a1e0::1",
  "fd7a:115c:a1e0:ffff:ffff:ffff:ffff:ffff",
]) {
  test(`allows ${address}`, () => {
    assert.equal(fromAllowedPeer(requestFrom(address)), true);
  });
}

for (const address of [
  "100.63.255.255",
  "100.128.0.0",
  "192.168.1.1",
  "10.0.0.1",
  "172.16.0.1",
  "::ffff:192.168.1.1",
  "::ffff:c0a8:101",
  "fd7a:115c:a1df::1",
  "fd7a:115c:a1e1::1",
  "fd00::1",
  "2001:db8::1",
  "::",
  "",
  "malformed",
  undefined,
]) {
  test(`rejects ${String(address)}`, () => {
    assert.equal(fromAllowedPeer(requestFrom(address)), false);
  });
}

test("forwarded headers cannot turn a LAN peer into an allowed peer", () => {
  const req = requestFrom("192.168.1.1");
  req.headers["x-forwarded-for"] = "100.64.0.1";
  req.headers["forwarded"] = "for=127.0.0.1";
  assert.equal(fromAllowedPeer(req), false);
});
