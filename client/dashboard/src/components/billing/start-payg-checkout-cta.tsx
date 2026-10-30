import {
  type PaygCheckoutEligibility,
  usePaygCheckoutAccess,
} from "@/components/billing/payg-checkout-access";
import { useSession } from "@/contexts/Auth";
import { useNavigate } from "react-router";
import { Button } from "@/components/ui/Button";
import { ButtonSize } from "@/components/ui/lib/types";
import { cn } from "@/lib/utils";

const CTA_LABEL = "Start pay as you go";

/** Navigation is shared by trial, billing, and lockout entrypoints. */
export function StartPaygCheckoutCTA({
  size = "md",
  className,
  label = CTA_LABEL,
  eligibility = "active-trial",
}: {
  size?: ButtonSize;
  className?: string;
  /** Customer-facing action for the surface that owns this shared behavior. */
  label?: string;
  eligibility?: PaygCheckoutEligibility;
}): JSX.Element | null {
  const { eligible } = usePaygCheckoutAccess(eligibility);
  const { organization } = useSession();
  const navigate = useNavigate();
  if (!eligible || !organization?.slug) return null;
  return (
    <div className={cn("flex flex-col items-start gap-2", className)}>
      <Button
        variant="primary"
        size={size}
        onClick={() => {
          void navigate(`/${organization.slug}/billing/setup`);
        }}
      >
        {label}
      </Button>
    </div>
  );
}
