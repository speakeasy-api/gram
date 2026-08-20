import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useOrganization, useSession } from "@/contexts/Auth.tsx";

const shouldShowImpersonationBanner = ({
  organizationSlug,
  impersonatorEmail,
  organizationOverride,
}: {
  organizationSlug: string;
  impersonatorEmail?: string;
  organizationOverride: boolean;
}): boolean =>
  organizationSlug === DEMO_ORG_SLUG ||
  !!impersonatorEmail ||
  organizationOverride;

export const useShowsImpersonationBanner = (): boolean => {
  const organization = useOrganization();
  const session = useSession();
  return shouldShowImpersonationBanner({
    organizationSlug: organization.slug,
    impersonatorEmail: session.impersonatorEmail,
    organizationOverride: session.organizationOverride,
  });
};
