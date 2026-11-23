import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ToolSelectionPanel,
  type ToolSelectionToolRef,
} from "./ToolSelectionPanel";

afterEach(cleanup);

describe("ToolSelectionPanel tool rows", () => {
  it.each([false, true])(
    "uses one named checkbox and toggles once per action (flat: %s)",
    async (flat) => {
      const user = userEvent.setup();
      const onChange = vi.fn();
      function Harness() {
        const [selectedTools, setSelectedTools] = useState<
          ToolSelectionToolRef[]
        >([]);
        return (
          <ToolSelectionPanel
            servers={[
              {
                id: "server",
                name: "Example server",
                status: "ready",
                tools: [{ name: "search_records", disposition: "unannotated" }],
              },
            ]}
            mode="tools"
            selectedAnnotations={[]}
            selectedTools={selectedTools}
            flattenSingleServer={flat}
            onSelectionChange={(change) => {
              onChange(change);
              setSelectedTools(change.tools);
            }}
          />
        );
      }
      const { container } = render(<Harness />);
      const checkbox = screen.getByRole("checkbox", { name: "search_records" });
      expect(container.querySelector("button button")).toBeNull();
      expect(checkbox.getAttribute("aria-checked")).toBe("false");

      await user.click(screen.getByText("search_records"));
      expect(onChange).toHaveBeenCalledTimes(1);
      expect(checkbox.getAttribute("aria-checked")).toBe("true");
      expect(document.activeElement).toBe(checkbox);

      await user.click(checkbox);
      expect(onChange).toHaveBeenCalledTimes(2);
      expect(checkbox.getAttribute("aria-checked")).toBe("false");

      await user.tab({ shift: true });
      await user.tab();
      expect(document.activeElement).toBe(checkbox);
      await user.keyboard(" ");
      expect(onChange).toHaveBeenCalledTimes(3);
      expect(checkbox.getAttribute("aria-checked")).toBe("true");
      expect(document.activeElement).toBe(checkbox);
      expect(onChange).toHaveBeenLastCalledWith({
        kind: "tool-toggle",
        mode: "tools",
        annotations: [],
        tools: [{ serverId: "server", toolName: "search_records" }],
        serverId: "server",
        toolName: "search_records",
        selected: true,
      });
    },
  );
});

describe("ToolSelectionPanel annotation rows", () => {
  it("counts each tool once, under the disposition the server matches it by", () => {
    render(
      <ToolSelectionPanel
        servers={[
          {
            id: "server",
            name: "Example server",
            status: "ready",
            tools: [
              { name: "list_records", disposition: "read_only" },
              { name: "create_record", disposition: "unannotated" },
            ],
          },
        ]}
        mode="annotations"
        selectedAnnotations={[]}
        selectedTools={[]}
        onSelectionChange={() => {}}
      />,
    );

    expect(
      screen.getByRole("checkbox", { name: /Read-only.*1 tools/ }),
    ).toBeTruthy();
    expect(screen.queryByRole("checkbox", { name: /Idempotent/ })).toBeNull();
  });
});
