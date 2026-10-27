/** Keep the full bucket grid and tooltips, but label only a few overview ticks. */
export function overviewLabels(labels: string[]): string[] {
  const stride = Math.max(1, Math.ceil(labels.length / 4));
  return labels.map((label, index) =>
    index % stride === 0 ? label.split(",")[0]! : "",
  );
}
