import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Dialog } from "@/components/ui/Dialog";
import { RemoveMcpSourceDialogContent } from "./RemoveMcpSourceDialog";

vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: () => ({
    data: { mcpEndpoints: [{ mcpServerId: "other-1", slug: "other-one" }] },
    isError: false,
    isLoading: false,
  }),
}));

afterEach(cleanup);

const other = {
  id: "other-1",
  name: "Other server",
  visibility: "private",
} as McpServer;

function show(onConfirm = vi.fn().mockResolvedValue(undefined)) {
  render(
    <Dialog open>
      <Dialog.Content>
        <RemoveMcpSourceDialogContent
          title="Delete remote MCP server"
          entityDescription="the remote MCP source"
          serverName="Linear"
          successMessage="Deleted"
          failureMessage="Failed"
          linkedMcpServers={[other]}
          isPending={false}
          errorMessage={undefined}
          onClose={() => {}}
          onSuccess={() => {}}
          onConfirm={onConfirm}
        />
      </Dialog.Content>
    </Dialog>,
  );
  return onConfirm;
}

describe("RemoveMcpSourceDialogContent", () => {
  it("arms the delete only once the server name is typed", () => {
    const onConfirm = show();
    const remove = screen.getByRole("button", { name: "Delete" });
    expect(remove.hasAttribute("disabled")).toBe(true);
    const input = screen.getByRole("textbox", {
      name: "Type the server name to confirm",
    });
    fireEvent.change(input, { target: { value: "Linea" } });
    expect(remove.hasAttribute("disabled")).toBe(true);
    fireEvent.change(input, { target: { value: "Linear" } });
    expect(remove.hasAttribute("disabled")).toBe(false);
    fireEvent.click(remove);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("lists the other servers and says it cannot be undone once", () => {
    show();
    expect(screen.getByText("Other server")).toBeTruthy();
    expect(screen.getByText(/which will be deleted too/)).toBeTruthy();
    expect(screen.getAllByText(/cannot be undone/)).toHaveLength(1);
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
