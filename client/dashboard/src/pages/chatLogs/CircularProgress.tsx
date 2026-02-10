import { cn } from "@/lib/utils";

interface CircularProgressProps {
  score: number; // 0-100
  status: "success" | "failure" | "partial";
  size?: "sm" | "md" | "lg";
}

export function CircularProgress({
  score,
  status,
  size = "md",
}: CircularProgressProps) {
  const sizeMap = { sm: 40, md: 48, lg: 64 };
  const strokeWidthMap = { sm: 3, md: 4, lg: 5 };

  const dimension = sizeMap[size];
  const strokeWidth = strokeWidthMap[size];
  const radius = (dimension - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;
  const offset = circumference - (score / 100) * circumference;

  const colorMap = {
    success: "stroke-green-500",
    failure: "stroke-red-500",
    partial: "stroke-yellow-500",
  };

  return (
    <div className="relative inline-flex items-center justify-center">
      <svg width={dimension} height={dimension} className="-rotate-90">
        {/* Background circle */}
        <circle
          cx={dimension / 2}
          cy={dimension / 2}
          r={radius}
          strokeWidth={strokeWidth}
          className="stroke-muted fill-none"
        />
        {/* Progress circle */}
        <circle
          cx={dimension / 2}
          cy={dimension / 2}
          r={radius}
          strokeWidth={strokeWidth}
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          strokeLinecap="round"
          className={cn(
            "fill-none transition-all duration-300",
            colorMap[status],
          )}
        />
      </svg>
      <span className="absolute text-sm font-medium">{score}%</span>
    </div>
  );
}
