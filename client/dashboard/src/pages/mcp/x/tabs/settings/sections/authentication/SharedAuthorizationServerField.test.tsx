import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { SharedAuthorizationServerField } from "./SharedAuthorizationServerField";

function issuer(overrides: Partial<UserSessionIssuer>): UserSessionIssuer {
  return {
    id: "issuer-1",
    slug: "issuer",
    authnChallengeMode: "interactive",
    authorizationServerMode: "shared",
    clientIdMetadataAdmissionMode: "open",
    sessionDurationHours: 24,
    useAuthenticationHost: false,
    issuerUrlPinned: true,
    organizationId: "org-1",
    projectId: "",
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  };
}

afterEach(() => {
  cleanup();
});

describe("SharedAuthorizationServerField", () => {
  it("shows the values another platform needs", () => {
    render(
      <TooltipProvider>
        <SharedAuthorizationServerField
          userSessionIssuer={issuer({
            issuerUrl: "https://app.example.com/oauth/usi/issuer-1",
            tokenEndpoint: "https://app.example.com/oauth/usi/issuer-1/token",
          })}
          mcpUrl="https://app.example.com/mcp/server"
        />
      </TooltipProvider>,
    );

    expect(
      screen.getByText("https://app.example.com/oauth/usi/issuer-1"),
    ).toBeDefined();
    expect(
      screen.getByText("https://app.example.com/oauth/usi/issuer-1/token"),
    ).toBeDefined();
    expect(
      screen.getByText("https://app.example.com/mcp/server"),
    ).toBeDefined();
  });

  it("says when the shared authorization server is not served", () => {
    render(
      <SharedAuthorizationServerField
        userSessionIssuer={issuer({
          authorizationServerUnavailableReason: "host_not_served",
        })}
        mcpUrl={undefined}
      />,
    );

    expect(
      screen.getByText(/isn't available on this deployment/),
    ).toBeDefined();
  });

  it("stays out of the way for a per-server authorization server", () => {
    const { container } = render(
      <SharedAuthorizationServerField
        userSessionIssuer={issuer({ authorizationServerMode: "endpoint" })}
        mcpUrl={undefined}
      />,
    );

    expect(container.textContent).toBe("");
  });

  it("leaves out the resource while the server's URL is unknown", () => {
    render(
      <TooltipProvider>
        <SharedAuthorizationServerField
          userSessionIssuer={issuer({
            issuerUrl: "https://app.example.com/oauth/usi/issuer-1",
            tokenEndpoint: "https://app.example.com/oauth/usi/issuer-1/token",
          })}
          mcpUrl={undefined}
        />
      </TooltipProvider>,
    );

    expect(screen.queryByText("Resource")).toBeNull();
  });
});
