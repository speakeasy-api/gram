import {
  cleanup,
  fireEvent,
  render as renderWithoutProviders,
  screen,
} from "@testing-library/react";
import type { ReactElement } from "react";
import { afterEach, describe, expect, it } from "vitest";

import { TooltipProvider } from "@/components/ui/Tooltip";

import {
  CustomDomainDnsRecords,
  RecommendedDnsUpdateAlert,
} from "./CustomDomainDnsRecords";

afterEach(cleanup);

function render(ui: ReactElement) {
  return renderWithoutProviders(<TooltipProvider>{ui}</TooltipProvider>);
}

const target = "abcdefgh23.domains.example.net";
const sharedTarget = "cname.example.net.";
const aRecords = ["192.0.2.10", "192.0.2.11"];

function renderRecords(
  overrides: {
    domain?: string;
    dnsTarget?: string;
    suggestedRecordType?: "cname" | "a";
    aRecords?: string[];
  } = {},
) {
  render(
    <CustomDomainDnsRecords
      domain={{
        domain: overrides.domain ?? "mcp.example.com",
        organizationId: "org_example",
        dnsTarget: overrides.dnsTarget,
        suggestedRecordType: overrides.suggestedRecordType ?? "cname",
      }}
      aRecords={overrides.aRecords ?? []}
      sharedCnameTarget={sharedTarget}
    />,
  );
}

describe("CustomDomainDnsRecords", () => {
  it("shows a subdomain its designated CNAME target and TXT record", () => {
    renderRecords({ dnsTarget: target });

    expect(screen.getByText(/pointing to its designated domain/)).toBeTruthy();
    expect(screen.getByText(target)).toBeTruthy();
    expect(screen.queryByText(sharedTarget)).toBeNull();
    expect(screen.getByText("_gram.mcp.example.com")).toBeTruthy();
    expect(
      screen.getByText("gram-domain-verify=mcp.example.com,org_example"),
    ).toBeTruthy();
  });

  it("falls back to the shared CNAME when the domain has no designated target", () => {
    renderRecords();

    expect(screen.getByText(sharedTarget)).toBeTruthy();
    expect(screen.queryByText(/designated domain/)).toBeNull();
  });

  it("recommends CNAME flattening for an apex domain and keeps A records as the less flexible option", () => {
    renderRecords({
      domain: "example.com",
      dnsTarget: target,
      suggestedRecordType: "a",
      aRecords,
    });

    expect(
      screen.getByRole("button", { name: "CNAME flattening (recommended)" }),
    ).toBeTruthy();
    expect(screen.getByText(/at the designated domain below/)).toBeTruthy();
    expect(screen.getByText(target)).toBeTruthy();
    expect(screen.queryByText(aRecords[0]!)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "A records" }));

    expect(screen.getByText(/They're less flexible/)).toBeTruthy();
    expect(screen.getByText(aRecords[0]!)).toBeTruthy();
    expect(screen.getByText(aRecords[1]!)).toBeTruthy();
  });

  it("keeps the current apex guidance when per-domain targets are not configured", () => {
    renderRecords({
      domain: "example.com",
      suggestedRecordType: "a",
      aRecords,
    });

    expect(
      screen.getByRole("button", { name: "CNAME (subdomain)" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "A record (apex domain)" }),
    ).toBeTruthy();
    expect(screen.getByText(aRecords[0]!)).toBeTruthy();
    expect(screen.queryByText(/CNAME flattening/)).toBeNull();
  });
});

describe("RecommendedDnsUpdateAlert", () => {
  it("asks the domain to point its CNAME record at the designated domain", () => {
    render(
      <RecommendedDnsUpdateAlert
        domain={{
          domain: "mcp.example.com",
          dnsTarget: target,
        }}
      />,
    );

    expect(screen.getByText("Recommended update")).toBeTruthy();
    expect(screen.getByText(/Update the CNAME record for/)).toBeTruthy();
    expect(
      screen.getByText(/move your domain without DNS changes in future/),
    ).toBeTruthy();
    expect(screen.getByText(target)).toBeTruthy();
  });
});

it("explains flattening at a zone root without configured A records", () => {
  renderRecords({ domain: "example.co.uk", dnsTarget: target });
  expect(screen.getByText(/If this is your DNS zone root/)).toBeTruthy();
  expect(screen.getByText(target)).toBeTruthy();
});

it("does not label a subdomain's shared-target A fallback as a zone root", () => {
  renderRecords({ aRecords });
  fireEvent.click(screen.getByRole("button", { name: "A records" }));
  expect(screen.getByText(aRecords[0]!)).toBeTruthy();
  expect(screen.queryByText(/the zone root/)).toBeNull();
});
