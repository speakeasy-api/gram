import { spawn } from "node:child_process";
import path from "node:path";
import { createInterface } from "node:readline";
import type {
  ModelReply,
  RecordedMcpRequest,
  RecordedModelRequest,
} from "./chatTestServer";

/**
 * The chat test server, running in a child Node process. Use this from
 * DOM-environment tests; Node-environment tests use startChatTestServer
 * directly.
 */
export type ChatTestServerProcess = {
  baseUrl: string;
  mcpUrl: string;
  queueModelReplies: (...replies: ModelReply[]) => Promise<void>;
  /** Requests the server has received so far. */
  requests: () => Promise<{
    modelRequests: RecordedModelRequest[];
    mcpRequests: RecordedMcpRequest[];
  }>;
  reset: () => Promise<void>;
  close: () => Promise<void>;
};

export async function startChatTestServerProcess(): Promise<ChatTestServerProcess> {
  const child = spawn(
    process.execPath,
    [path.join(import.meta.dirname, "chatTestServerMain.ts")],
    { stdio: ["pipe", "pipe", "inherit"] },
  );

  const baseUrl = await new Promise<string>((resolve, reject) => {
    const lines = createInterface({ input: child.stdout });
    lines.once("line", (line) => {
      lines.close();
      resolve((JSON.parse(line) as { baseUrl: string }).baseUrl);
    });
    child.once("error", reject);
    child.once("exit", (code) =>
      reject(new Error(`chat test server exited with code ${code}`)),
    );
  });

  const control = async (route: string, body?: unknown) => {
    const response = await fetch(`${baseUrl}/__test/${route}`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!response.ok) {
      throw new Error(`chat test server ${route}: ${response.status}`);
    }
    return (await response.json()) as Awaited<
      ReturnType<ChatTestServerProcess["requests"]>
    >;
  };

  return {
    baseUrl,
    mcpUrl: `${baseUrl}/mcp`,
    queueModelReplies: async (...replies) => {
      await control("queue", replies);
    },
    requests: () => control("state"),
    reset: async () => {
      await control("reset");
    },
    close: () =>
      new Promise<void>((resolve) => {
        if (child.exitCode !== null) {
          resolve();
          return;
        }
        child.once("exit", () => resolve());
        child.kill("SIGTERM");
      }),
  };
}
