export default function debounce<Args extends unknown[]>(
  func: (...args: Args) => void,
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
