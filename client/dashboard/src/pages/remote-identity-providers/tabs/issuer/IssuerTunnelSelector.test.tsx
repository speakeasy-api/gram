import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const isPlatformAdmin = vi.fn();
const listProjects = vi.fn();
const listTunnels = vi.fn();
const onChange = vi.fn<(value: string) => void>();

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => isPlatformAdmin(),
  useOrganization: () => ({ id: "org_1" }),
}));

vi.mock("@gram/client/react-query/listProjects.js", () => ({
  useListProjects: (...args: unknown[]) => listProjects(...args),
}));

vi.mock("@gram/client/react-query/tunneledMcpServers.js", () => ({
  useTunneledMcpServers: (...args: unknown[]) => listTunnels(...args),
}));

import { IssuerTunnelSelector } from "./IssuerTunnelSelector";

afterEach(() => {
  cleanup();
  isPlatformAdmin.mockReset();
  listProjects.mockReset();
  listTunnels.mockReset();
  onChange.mockReset();
});

function setQueryResults(): void {
  listProjects.mockReturnValue({
    data: {
      projects: [{ id: "project_1", name: "Project One", slug: "default" }],
    },
  });
  listTunnels.mockReturnValue({
    data: { tunneledMcpServers: [] },
    error: null,
    isPending: false,
  });
}

describe("IssuerTunnelSelector", () => {
  it("does not render or fetch tunnels for non-platform admins", () => {
    isPlatformAdmin.mockReturnValue(false);
    setQueryResults();

    render(
      <IssuerTunnelSelector
        projectId="project_1"
        value=""
        onChange={onChange}
      />,
    );

    expect(screen.queryByText("OAuth network route")).toBeNull();
    expect(listTunnels.mock.calls[0]?.[2]).toMatchObject({ enabled: false });
  });

  it("does not render for organization-level issuers", () => {
    isPlatformAdmin.mockReturnValue(true);
    setQueryResults();

    render(<IssuerTunnelSelector projectId="" value="" onChange={onChange} />);

    expect(screen.queryByText("OAuth network route")).toBeNull();
    expect(listTunnels.mock.calls[0]?.[2]).toMatchObject({ enabled: false });
  });

  it("loads the issuer project's tunnels for a platform admin", () => {
    isPlatformAdmin.mockReturnValue(true);
    setQueryResults();

    render(
      <IssuerTunnelSelector
        projectId="project_1"
        value=""
        onChange={onChange}
      />,
    );

    expect(screen.getByText("Platform Admin Only")).toBeTruthy();
    expect(screen.getByText("OAuth network route")).toBeTruthy();
    expect(listTunnels.mock.calls[0]?.[0]).toEqual({
      gramProject: "default",
    });
    expect(listTunnels.mock.calls[0]?.[2]).toMatchObject({ enabled: true });
  });
});
