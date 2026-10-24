import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { KeyCredentialChoice } from "./KeyCredentialChoice";

const mocks = vi.hoisted(() => ({ choices: [] as unknown[] }));
vi.mock("@gram/client/react-query/mcpServerAuthClientChoices.js", () => ({
  useMcpServerAuthClientChoices: () => ({ data: { choices: mocks.choices } }),
}));

afterEach(cleanup);

describe("KeyCredentialChoice", () => {
  it("asks nothing of a server with one credential", () => {
    mocks.choices = [
      { id: "a", name: "Token", clientType: "static_credential" },
    ];
    const { container } = render(
      <KeyCredentialChoice
        serverId="s"
        serverName="GitHub"
        value={undefined}
        onChange={vi.fn()}
      />,
    );
    expect(container.innerHTML).toBe("");
  });

  it("asks which credential to use when a server offers several", () => {
    mocks.choices = [
      { id: "a", name: "Engineering token", clientType: "static_credential" },
      { id: "b", name: "Marketing", clientType: "service_account" },
    ];
    const onChange = vi.fn();
    render(
      <KeyCredentialChoice
        serverId="s"
        serverName="GitHub"
        value={undefined}
        onChange={onChange}
      />,
    );
    fireEvent.click(
      screen.getByRole("combobox", {
        name: "Credential the key uses on GitHub",
      }),
    );
    fireEvent.click(
      screen.getByRole("option", { name: /Marketing · Service account/ }),
    );
    expect(onChange).toHaveBeenCalledWith("b");
  });
});
