export type { UserConfig } from "./config.ts";
export { defineConfig } from "./config.ts";

const lazy = <T extends (...args: any[]) => any>(load: () => Promise<T>): T => {
  let fn: T;
  return (async (...args) => {
    fn ??= await load();
    return fn(...args);
  }) as T;
};

export const buildMCPServer = lazy(() =>
  import("./mcp.ts").then((m) => m.buildMCPServer),
);

export const buildFunctions = lazy(() =>
  import("./gram.ts").then((m) => m.buildFunctions),
);
