import assert from "node:assert/strict";
import { test } from "node:test";

import { frameRpcs, type Deps } from "./run.ts";

test("frameRpcs picks rpcs seen on more than half the routes", () => {
  const deps: Deps = {
    commit: "c",
    recorded_at: "t",
    routes: {
      a: {
        key: "a",
        path: "/a",
        title: "A",
        final_path: "/a",
        rpcs: ["/rpc/auth.info", "/rpc/x.list"],
      },
      b: {
        key: "b",
        path: "/b",
        title: "B",
        final_path: "/b",
        rpcs: ["/rpc/auth.info", "/rpc/y.list"],
      },
      c: {
        key: "c",
        path: "/c",
        title: "C",
        final_path: "/c",
        rpcs: ["/rpc/auth.info", "/rpc/x.list", "/rpc/x.list"],
      },
      d: { key: "d", path: "/d", title: "D", final_path: "/d", rpcs: [] },
    },
  };
  const frame = frameRpcs(deps);
  assert.deepEqual([...frame], ["/rpc/auth.info"]);
  assert.deepEqual([...frameRpcs({ ...deps, routes: {} })], []);
});
