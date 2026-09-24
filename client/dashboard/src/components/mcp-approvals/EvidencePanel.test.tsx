import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { EvidenceCapability, EvidenceDocument } from "./evidence";
import { EvidencePanel } from "./EvidencePanel";
import { evidenceSectionAnchor } from "./signals";

afterEach(cleanup);

function renderPanel(document: EvidenceDocument | null) {
  return render(
    <TooltipProvider>
      <EvidencePanel document={document} />
    </TooltipProvider>,
  );
}

function tool(
  name: string,
  overrides: Partial<EvidenceCapability> = {},
): EvidenceCapability {
  return {
    tool: name,
    declared: [],
    schemaImplied: [],
    actsOnBehalf: false,
    unannotated: true,
    ...overrides,
  };
}

function remoteDocument(
  overrides: Partial<EvidenceDocument> = {},
): EvidenceDocument {
  return {
    identity: {
      kind: "remote",
      versionPinned: false,
      host: "mcp.example.com",
      registrableDomain: "example.com",
    },
    packageNotPublished: false,
    repositoryNotFound: false,
    capabilities: [],
    gaps: [],
    ...overrides,
  };
}

/** Enough tools to cross the threshold where the listing gains its controls. */
function manyTools(): EvidenceCapability[] {
  return [
    tool("delete_everything", {
      declared: ["destructive"],
      actsOnBehalf: true,
      unannotated: false,
    }),
    tool("run_shell", {
      schemaImplied: ["arbitrary_command"],
      actsOnBehalf: true,
      unannotated: false,
    }),
    tool("create_item", { actsOnBehalf: true, unannotated: false }),
    tool("read_a"),
    tool("read_b"),
    tool("read_c"),
    tool("read_d"),
    tool("read_e"),
    tool("read_f"),
  ];
}

describe("EvidencePanel", () => {
  it("says nothing is known when nothing was gathered", () => {
    renderPanel(null);
    expect(screen.getByText(/No evidence gathered/)).toBeTruthy();
  });

  it("anchors each question so a signal can link to it", () => {
    const { container } = renderPanel(remoteDocument());
    for (const section of ["trust", "handover", "maturity", "capabilities"]) {
      expect(
        container.querySelector(`#${evidenceSectionAnchor(section as never)}`),
      ).toBeTruthy();
    }
  });

  it("drops the advisory question for a hosted endpoint", () => {
    // The database indexes packages; for a URL the group only ever said so.
    renderPanel(remoteDocument());
    expect(screen.queryByText(/say it's vulnerable/)).toBeNull();
  });

  it("keeps the advisory question for a package", () => {
    renderPanel(
      remoteDocument({
        identity: { kind: "package", versionPinned: true, packageName: "x" },
        advisories: { knownCount: 0, advisories: [] },
      }),
    );
    expect(screen.getByText(/say it's vulnerable/)).toBeTruthy();
  });

  it("states a missing domain registration as the registry's answer", () => {
    renderPanel(
      remoteDocument({
        domain: { domain: "example.com", unregistered: true },
      }),
    );
    expect(screen.getByText("no registration on file")).toBeTruthy();
  });

  it("orders the declared tools by how much authority they declare", () => {
    renderPanel(remoteDocument({ capabilities: manyTools() }));

    const rows = screen.getAllByRole("listitem");
    const names = rows.map((row) => row.textContent ?? "");
    expect(names[0]).toContain("delete_everything");
    expect(names[1]).toContain("run_shell");
  });

  it("narrows the tool listing by kind and says how much it is hiding", () => {
    renderPanel(remoteDocument({ capabilities: manyTools() }));

    fireEvent.click(screen.getByRole("button", { name: /Destructive 1/ }));

    expect(screen.getByText("Showing 1 of 9 declared tools.")).toBeTruthy();
    expect(screen.queryByText("read_a")).toBeNull();
    expect(screen.getByText("delete_everything")).toBeTruthy();
  });

  it("says nothing about the count while the whole listing is shown", () => {
    // The line exists to admit that rows are hidden. Left up when the filter
    // is cleared it would claim the opposite of what the list is doing.
    renderPanel(remoteDocument({ capabilities: manyTools() }));

    expect(screen.queryByText(/declared tools\./)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Destructive 1/ }));
    expect(screen.getByText(/declared tools\./)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /All 9/ }));
    expect(screen.queryByText(/declared tools\./)).toBeNull();
  });

  it("narrows the tool listing by name", () => {
    renderPanel(remoteDocument({ capabilities: manyTools() }));

    fireEvent.change(screen.getByPlaceholderText("Find a tool"), {
      target: { value: "shell" },
    });

    expect(screen.getByText("Showing 1 of 9 declared tools.")).toBeTruthy();
    expect(screen.getByText("run_shell")).toBeTruthy();
  });

  it("says the tools still exist when a filter matches none of them", () => {
    renderPanel(remoteDocument({ capabilities: manyTools() }));

    fireEvent.change(screen.getByPlaceholderText("Find a tool"), {
      target: { value: "nothing-matches-this" },
    });

    expect(
      screen.getByText(/No tool matches this filter\. The server declares 9\./),
    ).toBeTruthy();
  });

  it("leaves a short tool listing without controls to operate", () => {
    renderPanel(
      remoteDocument({ capabilities: [tool("read_a"), tool("read_b")] }),
    );
    expect(screen.queryByPlaceholderText("Find a tool")).toBeNull();
  });

  it("renders a declared-empty toolset as a declaration, not a failed check", () => {
    renderPanel(remoteDocument({ capabilitiesSource: "server" }));
    expect(
      screen.getByText(/answered the listing with zero tools/),
    ).toBeTruthy();
  });

  it("names the secrets a server asks for inside the authority frame", () => {
    renderPanel(
      remoteDocument({
        authority: {
          mode: "api_key",
          scopes: ["read"],
          dynamicRegistration: false,
          demandedSecrets: [
            { name: "ACME_TOKEN", required: true, description: "a token" },
          ],
          optionalSecrets: [{ name: "ACME_TEAM", required: false }],
          unauthenticatedTools: [],
          undeclared: false,
        },
      }),
    );

    const handover = screen
      .getByText("What is it asking me to hand over?")
      .closest("section");
    expect(handover).toBeTruthy();
    expect(within(handover!).getByText("ACME_TOKEN")).toBeTruthy();
    expect(within(handover!).getByText("ACME_TEAM")).toBeTruthy();
    expect(within(handover!).getByText("Secrets it requires")).toBeTruthy();
  });
});
