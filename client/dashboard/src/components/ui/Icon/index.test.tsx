import { render } from "@testing-library/react";
import * as lucide from "lucide-react";
import dynamicIconImports from "lucide-react/dynamicIconImports";
import { describe, expect, it } from "vitest";

import { Icon } from ".";
import type { IconName } from "./names";

const exportsByName: Readonly<Record<string, unknown>> = lucide;

function exportName(name: string): string {
  return name.replace(/(?:^|-)([a-z0-9])/g, (_match, char: string) =>
    char.toUpperCase(),
  );
}

describe("Icon", () => {
  // Icon resolves names on the lucide-react namespace, while IconName is typed
  // from dynamicIconImports. This holds the two together across upgrades.
  it("finds a lucide-react export for every icon name", () => {
    const missing = Object.keys(dynamicIconImports).filter(
      (name) => exportsByName[exportName(name)] === undefined,
    );

    expect(missing).toEqual([]);
  });

  it("renders the named icon, including names with digits", () => {
    const names: IconName[] = ["plus", "arrow-down-0-1", "axis-3d"];

    for (const name of names) {
      const { container, unmount } = render(<Icon name={name} />);
      expect(container.querySelector("svg")).not.toBeNull();
      unmount();
    }
  });
});
