"use client";

import { useEffect, useRef } from "react";
import { PromptCard } from "./prompt.card";

// Colors from the gradient to use for each row
const GRADIENT_COLORS = [
  "#C83228", // Red
  "#FB873F", // Orange
  "#D2DC91", // Light green
  "#5A8250", // Green
  "#002314", // Dark green
  "#00143C", // Dark blue
  "#2873D7", // Blue
  "#9BC3FF", // Light blue
];

// Example prompts that showcase gram's capabilities
const EXAMPLE_PROMPTS = [
  // Row 1
  [
    { service: "hubspot", prompt: "give me the last 5 deals" },
    {
      service: "slack",
      prompt: "send a message to #engineering about the deployment",
    },
    { service: "github", prompt: "list all open PRs for the frontend repo" },
    {
      service: "stripe",
      prompt: "show me payments over $1000 from last month",
    },
  ],
  // Row 2
  [
    { service: "jira", prompt: "create a bug ticket for the login page" },
    {
      service: "salesforce",
      prompt: "find leads that haven't been contacted in 30 days",
    },
    { service: "zendesk", prompt: "summarize open support tickets" },
    {
      service: "notion",
      prompt: "create a new page with today's meeting notes",
    },
  ],
  // Row 3
  [
    { service: "google", prompt: "search for recent AI news" },
    { service: "asana", prompt: "show tasks due this week" },
    { service: "linear", prompt: "what are my current high priority issues?" },
    { service: "figma", prompt: "export all artboards as PNGs" },
  ],
  // Row 4
  [
    { service: "airtable", prompt: "add a new record to the customers table" },
    {
      service: "intercom",
      prompt: "show me conversations with angry customers",
    },
    {
      service: "mailchimp",
      prompt: "what was the open rate of our last campaign?",
    },
    { service: "todoist", prompt: "add a task to finish the presentation" },
  ],
  // Row 5
  [
    { service: "dropbox", prompt: "find all PDF files shared last week" },
    { service: "trello", prompt: "move all cards in 'In Progress' to 'Done'" },
    {
      service: "zoom",
      prompt: "schedule a meeting with the design team for tomorrow",
    },
    { service: "shopify", prompt: "show me top selling products this month" },
  ],
  // Row 6
  [
    {
      service: "gmail",
      prompt: "draft an email to the team about the new feature",
    },
    {
      service: "calendar",
      prompt: "schedule a meeting with marketing next Tuesday",
    },
    {
      service: "drive",
      prompt: "find documents shared with me in the last week",
    },
    { service: "sheets", prompt: "create a budget spreadsheet for Q3" },
  ],
  // Row 7
  [
    { service: "twitter", prompt: "compose a tweet about our product launch" },
    {
      service: "linkedin",
      prompt: "find job postings for frontend developers",
    },
    { service: "instagram", prompt: "schedule a post for tomorrow at 9am" },
    {
      service: "facebook",
      prompt: "create an ad campaign for our new service",
    },
  ],
  // Row 8
  [
    { service: "aws", prompt: "show me EC2 instances with high CPU usage" },
    { service: "vercel", prompt: "list deployments from the last 24 hours" },
    { service: "netlify", prompt: "check the status of my latest deployment" },
    { service: "heroku", prompt: "scale the web service to 5 dynos" },
  ],
  // Row 9
  [
    { service: "spotify", prompt: "create a playlist of focus music" },
    { service: "youtube", prompt: "find tutorials on React hooks" },
    { service: "netflix", prompt: "recommend sci-fi movies from the 90s" },
    { service: "twitch", prompt: "who's streaming design content right now" },
  ],
  // Row 10
  [
    {
      service: "uber",
      prompt: "schedule a ride to the airport tomorrow at 8am",
    },
    {
      service: "doordash",
      prompt: "order lunch from the Italian place nearby",
    },
    {
      service: "airbnb",
      prompt: "find cabins near Lake Tahoe for next weekend",
    },
    { service: "expedia", prompt: "search for flights to New York in August" },
  ],
];

// Duplicate the rows to ensure we have enough for the infinite scroll effect
const ALL_ROWS = [...EXAMPLE_PROMPTS, ...EXAMPLE_PROMPTS];

export function PromptsSection() {
  const containerRef = useRef<HTMLDivElement>(null);
  const sectionRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    // Set up the animation for each row
    const rows = container.querySelectorAll(".prompt-row");
    rows.forEach((row, index) => {
      const rowEl = row as HTMLElement;

      // Alternate direction for each row
      const isEven = index % 2 === 0;

      // Apply the appropriate animation class directly
      if (isEven) {
        rowEl.classList.add("animate-marquee-right");
      } else {
        rowEl.classList.add("animate-marquee-left");
        // Set initial position for odd rows
        rowEl.style.transform = "translateX(-50%)";
      }

      // Set a different animation duration for each row to create varied movement
      const duration = 120 + index * 10;
      rowEl.style.animationDuration = `${duration}s`;
    });

    // Set up hover effects for rows
    const hoverRows = container.querySelectorAll(".hover-row");
    hoverRows.forEach((row) => {
      row.addEventListener("mouseenter", () => {
        const cards = row.querySelectorAll(".prompt-card");
        cards.forEach((card) => {
          const cardEl = card as HTMLElement;
          const borderColor = cardEl.dataset["borderColor"];
          if (borderColor) {
            cardEl.style.borderColor = borderColor;
          }
        });
      });

      row.addEventListener("mouseleave", () => {
        const cards = row.querySelectorAll(".prompt-card");
        cards.forEach((card) => {
          const cardEl = card as HTMLElement;
          cardEl.style.borderColor = "#D1D1D1";
        });
      });
    });
  }, []);

  return (
    <div
      ref={sectionRef}
      className="relative w-full md:w-1/2 min-h-screen bg-[#F9F9F9] overflow-hidden"
    >
      {/* Add CSS for animations directly in the component */}
      <style>{`
        @keyframes marquee-right {
          from { transform: translateX(0%); }
          to { transform: translateX(-50%); }
        }
        
        @keyframes marquee-left {
          from { transform: translateX(-50%); }
          to { transform: translateX(0%); }
        }
        
        .animate-marquee-right {
          animation: marquee-right 120s linear infinite;
        }
        
        .animate-marquee-left {
          animation: marquee-left 120s linear infinite;
        }
      `}</style>

      {/* Angled grid container */}
      <div
        ref={containerRef}
        className="absolute inset-0 z-10 flex flex-col justify-center transform rotate-12 scale-[1.35]"
      >
        {ALL_ROWS.map((row, rowIndex) => {
          // Get color for this row (cycle through the colors)
          const colorIndex = rowIndex % GRADIENT_COLORS.length;
          const borderColor = GRADIENT_COLORS[colorIndex];

          return (
            <div
              key={rowIndex}
              className={`prompt-row flex whitespace-nowrap py-2 hover-row`}
              style={{
                willChange: "transform", // Optimize for animation performance
              }}
            >
              {row.map((prompt, promptIndex) => (
                <PromptCard
                  key={`${rowIndex}-${promptIndex}`}
                  service={prompt.service}
                  prompt={prompt.prompt}
                  className="mx-3 flex-shrink-0 prompt-card"
                  borderColor={borderColor}
                />
              ))}
              {/* Duplicate the row items to ensure seamless looping */}
              {row.map((prompt, promptIndex) => (
                <PromptCard
                  key={`${rowIndex}-${promptIndex}-dup`}
                  service={prompt.service}
                  prompt={prompt.prompt}
                  className="mx-3 flex-shrink-0 prompt-card"
                  borderColor={borderColor}
                />
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}
