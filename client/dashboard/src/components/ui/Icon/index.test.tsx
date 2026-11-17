import { cleanup, render } from "@testing-library/react";
import dynamicIconImports from "lucide-react/dynamicIconImports";
import { afterEach, describe, expect, it } from "vitest";

import { Icon } from ".";
import type { IconName } from "./names";

afterEach(cleanup);

describe("Icon", () => {
  // Icon resolves names on the lucide-react namespace, while IconName is typed
  // from dynamicIconImports. Rendering every name holds the two together
  // across lucide upgrades, including aliases and names with digits.
  it("renders an svg for every icon name", () => {
    const names = Object.keys(dynamicIconImports) as IconName[];
    expect(names.length).toBeGreaterThan(1000);

    const missing = names.filter((name) => {
      const { container, unmount } = render(<Icon name={name} />);
      const found = container.querySelector("svg") !== null;
      unmount();
      return !found;
    });

    expect(missing).toEqual([]);
  });
});
