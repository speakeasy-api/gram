import type { IncomingMessage, Server } from "node:http";
import type { AddressInfo } from "node:net";

/** Shared plumbing for tests that stand up a fake HTTP API on 127.0.0.1. */

export async function readBody(req: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks).toString("utf8");
}

/** Listens on a free loopback port and returns the server's base URL. */
export async function listenOnLoopback(server: Server): Promise<string> {
  await new Promise<void>((resolve) => {
    server.listen(0, "127.0.0.1", () => resolve());
  });
  const { port } = server.address() as AddressInfo;
  return `http://127.0.0.1:${port}`;
}

/** Closes the server, dropping open connections such as unfinished streams. */
export function closeServer(server: Server): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    server.closeAllConnections();
    server.close((error) => (error ? reject(error) : resolve()));
  });
}
