export function preventDefault(e: { preventDefault: () => void }): void {
  e.preventDefault();
}
