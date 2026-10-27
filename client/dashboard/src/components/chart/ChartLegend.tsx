import { Text } from "@/components/ui/Text";

/** Persistent labels for a chart whose native legend is hidden when compact. */
export function ChartLegend({
  datasets,
}: {
  datasets: ReadonlyArray<{ label?: string; backgroundColor?: unknown }>;
}): JSX.Element {
  return (
    <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1" aria-label="Series">
      {datasets.map((dataset) => (
        <li key={dataset.label} className="flex items-center gap-1.5">
          <span
            aria-hidden="true"
            className="size-2.5"
            style={{
              backgroundColor:
                typeof dataset.backgroundColor === "string"
                  ? dataset.backgroundColor
                  : undefined,
            }}
          />
          <Text small muted>
            {dataset.label}
          </Text>
        </li>
      ))}
    </ul>
  );
}
