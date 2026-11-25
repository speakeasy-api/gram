/** Failures as a percentage of calls, 0–100; 0 when there were no calls. */
export function errorRate(failures: number, calls: number): number {
  return calls > 0 ? (failures / calls) * 100 : 0;
}
