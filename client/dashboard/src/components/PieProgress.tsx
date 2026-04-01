import { cn } from "@/lib/utils";

interface PieProgressProps {
  /**
   * Progress value from 0 to 100
   */
  value: number;
  /**
   * Size of the circle in pixels
   */
  size?: number;
  /**
   * Additional className for the container
   */
  className?: string;
}

export function PieProgress({ value, size = 16, className }: PieProgressProps) {
  const radius = size / 2;
  const center = size / 2;

  // Color interpolation from red (0%) to yellow (50%) to green (100%)
  const getColor = (percent: number): string => {
    if (percent < 50) {
      // Red to yellow (0-50%)
      return `hsl(${percent * 1.2}, 70%, 50%)`;
    } else {
      // Yellow to green (50-100%)
      return `hsl(${60 + (percent - 50) * 0.6}, 70%, 45%)`;
    }
  };

  const color = getColor(value);

  // Calculate the pie slice path
  const getPath = (percent: number): string => {
    if (percent >= 100) {
      // Full circle
      return `M ${center},${center} m -${radius},0 a ${radius},${radius} 0 1,0 ${radius * 2},0 a ${radius},${radius} 0 1,0 -${radius * 2},0`;
    }
    if (percent <= 0) {
      return "";
    }

    const angle = (percent / 100) * 2 * Math.PI;
    const x = center + radius * Math.sin(angle);
    const y = center - radius * Math.cos(angle);
    const largeArc = percent > 50 ? 1 : 0;

    return `M ${center},${center} L ${center},${center - radius} A ${radius},${radius} 0 ${largeArc},1 ${x},${y} Z`;
  };

  const path = getPath(value);

  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      className={cn("shrink-0", className)}
    >
      {/* Background circle (full pie) */}
      <circle
        cx={center}
        cy={center}
        r={radius}
        fill="currentColor"
        className="text-muted opacity-20"
      />
      {/* Progress pie slice */}
      {path && (
        <path d={path} fill={color} className="transition-all duration-300" />
      )}
    </svg>
  );
}
