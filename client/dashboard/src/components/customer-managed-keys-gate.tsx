import { Page } from "@/components/page-layout";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import type { ReactNode } from "react";

// CustomerManagedKeysGate is the product-feature (entitlement) gate for every
// page under External Services, Encryption Keys and Signing Key Sets, mounted
// after the RBAC scope gate so no protected request fires for a visitor
// lacking the page scope. The sidebar entries are already hidden without the
// entitlement; this covers direct URLs, including deep links to a credential,
// key or key set detail page. A gated but authorized organization sees only
// the framed refusal.
//
// A failed entitlement read is treated as not-yet-known rather than as a
// refusal, so an entitled organization never flashes the gate; the server
// enforces the entitlement on every endpoint regardless.
export function CustomerManagedKeysGate({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  const organization = useOrganization();
  const { data: features, isLoading: featuresLoading } = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { staleTime: 30_000, throwOnError: false },
  );

  // Treat "still loading" and "the read failed" as not-yet-known so an
  // entitled organization never flashes the gate.
  const gated =
    !featuresLoading &&
    features?.customerManagedEncryptionKeysEnabled === false;

  if (gated) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <Text muted className="py-8 text-center">
            Customer-managed keys are not enabled for this organization.
          </Text>
        </Page.Body>
      </Page>
    );
  }

  return <>{children}</>;
}
