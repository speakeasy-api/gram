import { useMemo } from "react";

import { Type } from "@/components/ui/type";
import { Combobox } from "@/components/ui/combobox";
import { cn } from "@/lib/utils";

import { useEnvironments } from "../environments/Environments";

interface EnvironmentSelectorProps {
  selectedEnvironment: string | null;
  setSelectedEnvironment: (environment: string) => void;
  className?: string;
}

export function EnvironmentSelector({
  selectedEnvironment,
  setSelectedEnvironment,
  className,
}: EnvironmentSelectorProps) {
  const environments = useEnvironments();

  const allItems = useMemo(() => {
    return environments.map((environment) => ({
      label: environment.name,
      value: environment.slug,
    }));
  }, [environments]);

  const selectedEnvironmentData = useMemo(() => {
    return environments.find((env) => env.slug === selectedEnvironment);
  }, [environments, selectedEnvironment]);

  return (
    <Combobox
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
      className={cn("min-w-32", className)}
    >
      <Type variant="small" className="font-medium">
        {selectedEnvironmentData?.name || "Select environment"}
      </Type>
    </Combobox>
  );
}
