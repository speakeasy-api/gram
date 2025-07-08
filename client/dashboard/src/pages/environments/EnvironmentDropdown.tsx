import { Type } from "@/components/ui/type";
import { Combobox } from "@/components/ui/combobox";
import { useListEnvironments } from "@gram/client/react-query";
import { cn } from "@/lib/utils";

export function EnvironmentDropdown({
  selectedEnvironment,
  setSelectedEnvironment,
  visibilityThreshold = 0,
  className,
}: {
  selectedEnvironment: string | null;
  setSelectedEnvironment: (environment: string) => void;
  visibilityThreshold?: number;
  className?: string;
}) {
  const { data: environments } = useListEnvironments();

  const environmentDropdownItems =
    environments?.environments.map((environment) => ({
      ...environment,
      label: environment.name,
      value: environment.slug,
    })) ?? [];

  if (environmentDropdownItems.length < visibilityThreshold) {
    return null;
  }

  return (
    <Combobox
      items={environmentDropdownItems}
      selected={environmentDropdownItems.find(
        (item) => item.value === selectedEnvironment
      )}
      onSelectionChange={(value) => setSelectedEnvironment(value.value)}
      tooltip="Switch environments"
      className={cn("max-w-fit", className)}
    >
      <Type variant="small">{selectedEnvironment}</Type>
    </Combobox>
  );
}
