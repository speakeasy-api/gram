export function countTasksBelow(container: HTMLElement): number {
  if (container.scrollHeight <= container.clientHeight + 1) return 0;

  const viewportBottom = container.getBoundingClientRect().bottom;
  return Array.from(container.children).filter(
    (child) => child.getBoundingClientRect().bottom > viewportBottom + 1,
  ).length;
}
