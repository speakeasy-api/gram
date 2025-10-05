import { Type } from "@/components/ui/type";
import { Combobox } from "@/components/ui/combobox";
import { useListEnvironments } from "@gram/client/react-query";
import { cn } from "@/lib/utils";
import { useMemo } from "react";

export function EnvironmentDropdown({
  selectedEnvironment,
  setSelectedEnvironment,
  tooltip = "Switch environments",
  label,
  visibilityThreshold = 0,
  className,
}: {
  selectedEnvironment: string | null;
  setSelectedEnvironment: (environment: string) => void;
  tooltip?: string;
  label?: string;
  visibilityThreshold?: number;
  className?: string;
}) {
  const { data: environments } = useListEnvironments();

  const allItems = useMemo(() => {
    return (
      environments?.environments.map((environment) => ({
        label: environment.name,
        value: environment.slug,
      })) ?? []
    );
  }, [environments?.environments]);

  if (allItems.length < visibilityThreshold) {
    return null;
  }

  const selectedEnvironmentData = environments?.environments.find(
    (env) => env.slug === selectedEnvironment,
  );

  return (
    <Combobox
      label={label}
      items={allItems}
      selected={
        selectedEnvironmentData
          ? {
              label: selectedEnvironmentData.name,
              value: selectedEnvironmentData.slug,
            }
          : undefined
      }
      onSelectionChange={(item) => {
        setSelectedEnvironment(item.value);
      }}
      tooltip={tooltip}
      className={cn("max-w-fit", className)}
    >
      <Type variant="small">
        {selectedEnvironmentData?.name || selectedEnvironment}
      </Type>
    </Combobox>
  );
}
