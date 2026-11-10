import assert from "node:assert/strict";
import childProcess from "node:child_process";
import { registerHooks, syncBuiltinESMExports } from "node:module";
import { test } from "node:test";
import { promisify } from "node:util";

let generation = 0;

for (const [name, pluginID] of [
  ["pause-stack", "speakeasy.pause-stack"],
  ["worktrunk", "speakeasy.worktrunk"],
]) {
  for (const [description, value, enabled] of [
    ["resolved value enables the plugin", `other: ${pluginID} :another`, true],
    ["partial IDs do not enable the plugin", `${pluginID}-extra`, false],
    ["an empty value disables the plugin", "", false],
    ["an unset variable disables the plugin", undefined, false],
    ["an invalid value disables the plugin", 42, false],
    ["invalid JSON disables the plugin", "invalid-json", false],
    ["a failed lookup disables the plugin", "lookup-error", false],
  ]) {
    test(`${name}: ${description}`, async (t) => {
      const directory = "/worktree/plugin-location";
      const calls = [];
      const original = childProcess.execFile;
      childProcess.execFile = Object.assign(() => {}, {
        [promisify.custom]: async (file, args, options) => {
          calls.push({ file, args, options });
          assert.equal(file, "mise");
          assert.deepEqual(args, ["env", "--json"]);
          assert.equal(options.cwd, directory);
          if (value === "lookup-error") throw new Error("lookup failed");
          return {
            stdout:
              value === "invalid-json"
                ? "not JSON"
                : JSON.stringify({ GRAM_OPENCODE_PLUGINS: value }),
          };
        },
      });
      syncBuiltinESMExports();
      t.after(() => {
        childProcess.execFile = original;
        syncBuiltinESMExports();
      });

      // Enabling Worktrunk needs only this SDK type, not a running server.
      const hooks = registerHooks({
        resolve(specifier, context, nextResolve) {
          if (specifier === "@opencode/plugin") {
            return {
              url: "data:text/javascript,export const Worktree = { OperationError: Error };",
              shortCircuit: true,
            };
          }
          return nextResolve(specifier, context);
        },
      });
      t.after(() => hooks.deregister());

      const { default: plugin } = await import(
        `../plugins/${name}.js?enablement=${generation++}`
      );
      let activated = false;
      const cleanup = await plugin.setup({
        location: { directory, project: { canonical: "/canonical" } },
        storage: {},
        event: {
          async *subscribe() {
            activated = true;
          },
        },
        worktree: {
          async transform(register) {
            register({
              add(strategy) {
                assert.equal(strategy.id, pluginID);
                activated = true;
              },
            });
          },
        },
      });
      if (cleanup) t.after(cleanup);
      assert.equal(calls.length, 1);
      assert.equal(activated, enabled);
    });
  }
}
