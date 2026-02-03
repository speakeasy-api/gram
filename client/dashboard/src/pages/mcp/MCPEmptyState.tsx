import { EmptyState } from "@/components/page-layout";

export function MCPEmptyState({
  nonEmptyProjectCTA,
}: {
  nonEmptyProjectCTA?: React.ReactNode;
}) {
  return (
    <EmptyState
      heading="No MCP servers yet"
      description="Gram generates MCP-ready tools from your OpenAPI documents. Get a hosted MCP server in seconds, not days."
      graphic={<MCPEmptyGraphic />}
      nonEmptyProjectCTA={nonEmptyProjectCTA}
    />
  );
}

/**
 * Hand-drawn robot illustration for empty state
 * Shows a friendly robot thinking/waiting with a question mark
 */
export default function MCPEmptyGraphic() {
  return (
    <div className="w-full max-w-xs">
      <svg
        viewBox="0 0 200 180"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        className="w-full h-auto"
        aria-hidden="true"
      >
        {/* Robot thinking pose */}
        <g
          className="stroke-slate-700 dark:stroke-slate-300"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          {/* Robot head - slightly tilted for thinking pose */}
          <rect
            x="65"
            y="45"
            width="50"
            height="45"
            rx="5"
            fill="none"
            transform="rotate(-5 90 67)"
          />
          {/* Antenna */}
          <line x1="88" y1="42" x2="85" y2="28" />
          <circle cx="84" cy="24" r="4" fill="none" />

          {/* Screen face with curious expression */}
          <rect
            x="73"
            y="55"
            width="34"
            height="22"
            rx="3"
            className="fill-slate-200 dark:fill-slate-700"
            strokeWidth="1"
            transform="rotate(-5 90 66)"
          />
          {/* Eyes - looking up thoughtfully */}
          <circle
            cx="82"
            cy="64"
            r="3"
            className="fill-slate-600 dark:fill-slate-400"
          />
          <circle
            cx="98"
            cy="63"
            r="3"
            className="fill-slate-600 dark:fill-slate-400"
          />
          {/* Eye glints */}
          <circle cx="81" cy="63" r="1" className="fill-white" />
          <circle cx="97" cy="62" r="1" className="fill-white" />

          {/* Robot body */}
          <rect x="72" y="95" width="36" height="35" rx="4" fill="none" />
          {/* Body details */}
          <circle
            cx="82"
            cy="105"
            r="2.5"
            className="fill-slate-400 dark:fill-slate-500"
          />
          <circle
            cx="90"
            cy="105"
            r="2.5"
            className="fill-slate-400 dark:fill-slate-500"
          />
          <circle
            cx="98"
            cy="105"
            r="2.5"
            className="fill-slate-400 dark:fill-slate-500"
          />
          <rect
            x="78"
            y="113"
            width="24"
            height="8"
            rx="2"
            className="fill-slate-200 dark:fill-slate-700"
            strokeWidth="0.8"
          />

          {/* Left arm - hand on chin thinking pose */}
          <path d="M72 105 L55 100 L50 85" fill="none" />
          <circle cx="50" cy="82" r="5" fill="none" />
          {/* Hand touching chin area */}
          <path d="M50 77 L55 72 L60 75" fill="none" strokeWidth="1" />

          {/* Right arm - relaxed */}
          <path d="M108 108 L125 115 L130 125" fill="none" />
          <circle cx="132" cy="128" r="5" fill="none" />

          {/* Legs */}
          <line x1="82" y1="130" x2="80" y2="150" />
          <line x1="98" y1="130" x2="100" y2="150" />
          {/* Feet */}
          <rect x="72" y="150" width="16" height="7" rx="3" fill="none" />
          <rect x="92" y="150" width="16" height="7" rx="3" fill="none" />
        </g>

        {/* Question mark - floating above robot */}
        <g
          className="stroke-slate-500 dark:stroke-slate-400"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M135 35 Q145 25 145 40 Q145 50 135 55" fill="none" />
          <circle
            cx="135"
            cy="65"
            r="2"
            className="fill-slate-500 dark:fill-slate-400"
          />
        </g>

        {/* Thought bubbles leading to question mark */}
        <g className="fill-slate-400 dark:fill-slate-500">
          <circle cx="115" cy="45" r="3" />
          <circle cx="125" cy="38" r="4" />
        </g>

        {/* Small decorative elements - tools waiting to be connected */}
        <g
          className="stroke-slate-400 dark:stroke-slate-500"
          strokeWidth="1"
          fill="none"
          strokeLinecap="round"
        >
          {/* Small plug icon */}
          <g transform="translate(25, 110)">
            <rect x="0" y="2" width="12" height="8" rx="1" />
            <line x1="0" y1="5" x2="-4" y2="5" />
            <line x1="0" y1="8" x2="-4" y2="8" />
          </g>

          {/* Small gear icon */}
          <g transform="translate(160, 90)">
            <circle cx="8" cy="8" r="6" />
            <circle cx="8" cy="8" r="2" />
          </g>

          {/* Small server icon */}
          <g transform="translate(155, 130)">
            <rect x="0" y="0" width="16" height="20" rx="2" />
            <line x1="3" y1="5" x2="13" y2="5" />
            <line x1="3" y1="10" x2="13" y2="10" />
            <circle
              cx="5"
              cy="16"
              r="1.5"
              className="fill-slate-400 dark:fill-slate-500"
            />
          </g>
        </g>
      </svg>
    </div>
  );
}
