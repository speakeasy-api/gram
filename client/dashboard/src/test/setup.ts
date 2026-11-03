import { format } from "node:util";
import { beforeEach } from "vitest";

// happy-dom 21 rejects Animation.finished on cancel() without marking the
// promise handled, so animations that motion stops mid-flight surface as
// unhandled rejections. The Web Animations spec marks that promise handled.
// Remove once happy-dom fixes it: capricorn86/happy-dom#2412.
// Node-environment test files have no Animation global.
const originalCancel: unknown =
  typeof Animation === "undefined"
    ? undefined
    : Reflect.get(Animation.prototype, "cancel");
if (typeof originalCancel === "function") {
  Animation.prototype.cancel = function cancel(this: Animation) {
    this.finished.catch(() => undefined);
    Reflect.apply(originalCancel, this, []);
  };
}

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
