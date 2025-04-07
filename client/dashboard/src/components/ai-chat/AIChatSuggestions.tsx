import { useState } from "react";
import { Button } from "@speakeasy-api/moonshine";
import { ChevronDown, ChevronUp } from "lucide-react";
import { AIChatSuggestion } from "./types.ts";

interface AIChatSuggestionsProps {
  suggestions: AIChatSuggestion[];
  onSuggestionClick: (suggestion: AIChatSuggestion) => void;
}

export function AIChatSuggestions({
  suggestions,
  onSuggestionClick,
}: AIChatSuggestionsProps) {
  const [expanded, setExpanded] = useState(false);

  // Show only first 4 suggestions when not expanded
  const visibleSuggestions = expanded ? suggestions : suggestions.slice(0, 4);

  return (
    <div className="mb-4 w-full">
      <div className="mb-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
        {visibleSuggestions.map((suggestion, index) => (
          <Button
            key={index}
            variant="outline"
            className="hover:bg-primary hover:text-primary-foreground h-auto w-full justify-start break-words rounded-md px-3 py-2 text-left font-normal transition-colors"
            onClick={() => onSuggestionClick(suggestion)}
          >
            <span className="mr-2 text-lg">{suggestion.emoji}</span>
            <span className="whitespace-normal">{suggestion.text}</span>
          </Button>
        ))}
      </div>

      {suggestions.length > 4 && (
        <Button
          variant="ghost"
          size="sm"
          className="text-muted-foreground mt-1 w-full rounded-md"
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? (
            <>
              <ChevronUp className="mr-2 h-4 w-4" />
              Show fewer suggestions
            </>
          ) : (
            <>
              <ChevronDown className="mr-2 h-4 w-4" />
              Show more suggestions
            </>
          )}
        </Button>
      )}
    </div>
  );
}
