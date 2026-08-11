/**
 * Returns a version of `func` that only runs once `wait` ms have passed
 * without another call.
 */
export default function debounce<Args extends unknown[]>(
  func: (...args: Args) => unknown,
  wait: number,
): (...args: Args) => void {
  let timeout: ReturnType<typeof setTimeout>;

  return (...args: Args) => {
    clearTimeout(timeout);
    timeout = setTimeout(() => {
      func(...args);
    }, wait);
  };
}
