import { format } from "node:util";
import { beforeEach } from "vitest";

beforeEach((context) => {
  const originals = { error: console.error, warn: console.warn };
  const unexpected: string[] = [];
  for (const method of ["error", "warn"] as const) {
    // Plain wrappers survive restoreAllMocks; expected logs can use local spies.
    console[method] = (...args: unknown[]) => {
      originals[method].apply(console, args);
      unexpected.push(`console.${method}: ${format(...args)}`);
    };
  }

  // Check after cleanup, outside application try/catch blocks.
  context.onTestFinished(() => {
    console.error = originals.error;
    console.warn = originals.warn;
    if (unexpected.length > 0) {
      throw new Error(
        "Unexpected console output (fix the warning or explicitly spy on an expected call):\n" +
          unexpected.join("\n"),
      );
    }
  });
});
