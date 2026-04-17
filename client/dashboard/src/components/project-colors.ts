// Generate colors from project label
export function getProjectColors(label: string): {
  from: string;
  to: string;
  angle: number;
} {
  // FNV-1a hash function for better distribution
  const fnv1a = (str: string) => {
    let hash = 2166136261;
    for (let i = 0; i < str.length; i++) {
      hash ^= str.charCodeAt(i);
      hash +=
        (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
    }
    return hash >>> 0;
  };

  const hash = fnv1a(label);

  // Generate four random-ish numbers from the hash for more variation
  const n1 = hash % 360;
  const n2 = (hash >> 8) % 360;
  const n3 = (hash >> 16) % 100;
  const n4 = (hash >> 24) % 360; // For gradient angle

  const hue1 = n1;
  const hue2 = (hue1 + n2) % 360;
  const saturation = Math.max(65, n3);
  const angle = n4;

  return {
    from: `hsl(${hue1}, ${saturation}%, 65%)`,
    to: `hsl(${hue2}, ${saturation}%, 60%)`,
    angle,
  };
}
