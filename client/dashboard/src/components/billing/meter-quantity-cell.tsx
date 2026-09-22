import {
  formatMeterQuantity,
  formatScaledMeterQuantity,
} from "./meter-usage-adapter";

export function MeterQuantityCell({
  quantity,
  unit,
  showRaw,
}: {
  quantity: string;
  unit: string;
  showRaw: boolean;
}): JSX.Element {
  let usage: string;
  if (showRaw) {
    usage = formatMeterQuantity(quantity, unit, "standard");
  } else {
    usage = formatScaledMeterQuantity(quantity, unit);
  }

  return (
    <span className="block w-full text-right tabular-nums select-text">
      {usage}
    </span>
  );
}
