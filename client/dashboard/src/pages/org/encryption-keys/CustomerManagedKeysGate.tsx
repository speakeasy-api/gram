import { Page } from "@/components/page-layout";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import type { ReactNode } from "react";

// CustomerManagedKeysGate is the product-feature (entitlement) gate for every
// page under Encryption Keys and Signing Key Sets, mounted after the RBAC scope
// gate so no protected request fires for a visitor lacking the page scope. The
// sidebar entry is already hidden without the entitlement; this covers direct
// URLs, including deep links to a key or key set detail page. A gated but
// authorized organization sees only the framed refusal.
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
