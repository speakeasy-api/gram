import {
  createServer,
  type IncomingMessage,
  type Server,
  type ServerResponse,
} from "node:http";
import { createServer as createHTTPSServer } from "node:https";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { handleEMARequest, localTLS } from "./ema-upstream.ts";
import { handleMockRequest } from "./mock-server.ts";
import {
  parseScenarioPath,
  readJSONFile,
  scenarioFiles,
  scenarioKind,
  writeJSONFile,
  type ScenarioRequest,
} from "./scenario-store.ts";
import { handleWorkloadRequest, loadWorkloadTLS } from "./workload-issuer.ts";

export type HostInfo = {
  http: string;
  /** Trusted workload hostname, used by HTTPS mocks and workload issuers. */
  workload: string | { unavailable: string };
  /** Trusted localhost certificate, used by EMA upstreams. */
  localhost: string | { unavailable: string };
};

export function hostInfoPath(runDir: string) {
  return resolve(runDir, "host.json");
}

export function readHostInfo(runDir: string): HostInfo {
  const info = readJSONFile<HostInfo>(hostInfoPath(runDir));
  if (!info) throw new Error("ENV_MOCK_HOST: mock host has not started");
  return info;
}

export function hostOrigin(value: HostInfo["workload"]): string {
  if (typeof value !== "string") throw new Error(value.unavailable);
  return value;
}

/** Answers for any scenario under runDir and holds no scenario state itself. */
export async function startScenarioHost(
  runDir: string,
  options: { host?: string; port?: number; publish?: boolean } = {},
) {
  let closing = false;
  const servers: Server[] = [];
  const listener =
    (root: () => string) => (req: IncomingMessage, res: ServerResponse) => {
      const parsed = parseScenarioPath(
        new URL(req.url ?? "/", "http://mock.invalid").pathname,
      );
      const kind = parsed && scenarioKind(runDir, parsed.id);
      if (!parsed || !kind) {
        res.writeHead(404, { "Content-Type": "application/json" });
        res.end('{"error":"not_found"}');
        return;
      }
      const request: ScenarioRequest = {
        files: scenarioFiles(runDir, parsed.id),
        origin: `${root()}/s/${parsed.id}`,
        path: parsed.path,
        closing: () => closing,
      };
      if (kind === "ema") handleEMARequest(request, req, res);
      else if (kind === "workload") handleWorkloadRequest(request, req, res);
      else handleMockRequest(request, req, res);
    };
  const listen = async (
    server: Server,
    host: string,
    port: number,
    originFor: (port: number) => string,
    setRoot: (origin: string) => void,
  ) => {
    await new Promise<void>((done, reject) => {
      server.once("error", reject);
      server.listen(port, host, () => {
        server.off("error", reject);
        done();
      });
    });
    servers.push(server);
    const address = server.address();
    if (!address || typeof address === "string")
      throw new Error("Missing fixture server address");
    const origin = originFor(address.port);
    setRoot(origin);
    return origin;
  };

  async function close() {
    closing = true;
    await Promise.all(
      servers.map(
        (server) =>
          new Promise<void>((done, reject) => {
            server.close((error) => (error ? reject(error) : done()));
            server.closeAllConnections();
          }),
      ),
    );
  }
  try {
    let httpRoot = "";
    const httpHost = options.host ?? "127.0.0.1";
    const http = await listen(
      createServer(listener(() => httpRoot)),
      httpHost,
      options.port ?? 0,
      (port) =>
        `http://${httpHost.includes(":") ? `[${httpHost}]` : httpHost}:${port}`,
      (origin) => {
        httpRoot = origin;
      },
    );

    // A missing certificate only affects scenarios that need that listener.
    let workloadRoot = "";
    const workload = await loadWorkloadTLS().then(
      ({ hostname, cert, key }) =>
        listen(
          createHTTPSServer(
            { cert, key },
            listener(() => workloadRoot),
          ),
          "127.0.0.1",
          0,
          (port) => `https://${hostname}:${port}`,
          (origin) => {
            workloadRoot = origin;
          },
        ),
      (error: Error) => ({ unavailable: error.message }),
    );
    let localhostRoot = "";
    const localhost = await localTLS().then(
      ({ cert, key }) =>
        listen(
          createHTTPSServer(
            { cert, key },
            listener(() => localhostRoot),
          ),
          "127.0.0.1",
          0,
          (port) => `https://localhost:${port}`,
          (origin) => {
            localhostRoot = origin;
          },
        ),
      (error: Error) => ({ unavailable: error.message }),
    );

    const info: HostInfo = { http, workload, localhost };
    writeJSONFile(hostInfoPath(runDir), info);
    return {
      info,
      close,
    };
  } catch (error) {
    await close();
    throw error;
  }
}

/**
 * Connects a scenario to the suite's host. Outside a suite run, as in unit
 * tests and `mcp-auth:serve`, it starts a private host that close() removes.
 */
export async function acquireHost(options?: { host?: string; port?: number }) {
  const shared = process.env.MCP_AUTH_RUN_DIR;
  if (shared) {
    return {
      runDir: shared,
      info: readHostInfo(shared),
      release: async () => {},
    };
  }
  const runDir = mkdtempSync(join(tmpdir(), "mcp-auth-host-"));
  const host = await startScenarioHost(runDir, options).catch((error) => {
    rmSync(runDir, { recursive: true, force: true });
    throw error;
  });
  return {
    runDir,
    info: host.info,
    release: async () => {
      await host.close();
      rmSync(runDir, { recursive: true, force: true });
    },
  };
}
