import type { OnboardingOption } from "@gram/client/models/components/onboardingoption.js";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { MDM_TARGETS } from "@/pages/setup/components/mdm-targets";

const NOTES: Record<string, string> = {
  jamf: "Managed settings and the device agent can be pushed to every Mac.",
  intune:
    "Managed settings and the device agent can be pushed to macOS and Windows.",
  iru: "Managed settings and the device agent can be pushed to every Mac.",
  none: "Each person installs plugins themselves. You can add an MDM later.",
};

/** The device-management question: the vendor only, nothing about what it can push. */
export function MdmPicker({
  vendors,
  value,
  onChange,
  disabled = false,
}: {
  vendors: OnboardingOption[];
  value: string | null;
  onChange: (vendor: string) => void;
  disabled?: boolean;
}): JSX.Element {
  return (
    <RadioCardGroup value={value} onValueChange={onChange} disabled={disabled}>
      {vendors.map((vendor) => {
        const target = MDM_TARGETS.find(
          (candidate) => candidate.id === vendor.slug,
        );
        return (
          <RadioCard
            key={vendor.slug}
            value={vendor.slug}
            title={vendor.name}
            leading={
              target ? (
                <img
                  src={target.logo}
                  alt=""
                  className="h-8 w-8 object-contain"
                />
              ) : undefined
            }
          >
            {NOTES[vendor.slug] ?? ""}
          </RadioCard>
        );
      })}
    </RadioCardGroup>
  );
}
