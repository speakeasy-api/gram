import { MiniCard } from "@/components/ui/card-mini";
import { Heading } from "@/components/ui/heading";
import { cn, getServerURL } from "@/lib/utils";
import {
  useDeploymentLogsSuspense,
  useDeploymentSuspense,
} from "@gram/client/react-query";
import { Button, Icon, Badge, Input } from "@speakeasy-api/moonshine";
import { FileCodeIcon, CopyIcon, CheckIcon } from "lucide-react";
import { useParams } from "react-router";
import { ToolsList } from "./ToolsList";
import { useState, useMemo, useEffect, useCallback } from "react";
import type { DeploymentLogEvent } from "@gram/client/models/components";

type LogLevel = 'WARN' | 'INFO' | 'DEBUG' | 'ERROR' | 'OK';
type LogFocus = 'all' | 'warns' | 'errors';

interface ParsedLogEntry {
  timestamp?: string;
  level: LogLevel;
  message: string;
  originalMessage: string;
  originalEvent: string;
}

function parseLogMessage(message: string, event: string): ParsedLogEntry {
  // Common log patterns
  const patterns = [
    // ISO timestamp with level: 2024-01-15T10:30:45Z [ERROR] Message
    /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s*\[?(WARN|WARNING|INFO|DEBUG|ERROR|OK)\]?\s*(.*)$/i,
    // Simple timestamp with level: 10:30:45 ERROR: Message
    /^(\d{1,2}:\d{2}:\d{2}(?:\.\d+)?)\s*\[?(WARN|WARNING|INFO|DEBUG|ERROR|OK)\]?:?\s*(.*)$/i,
    // Level without timestamp: [ERROR] Message or ERROR: Message
    /^\[?(WARN|WARNING|INFO|DEBUG|ERROR|OK)\]?:?\s+(.*)$/i,
  ];

  for (const pattern of patterns) {
    const match = message.match(pattern);
    if (match) {
      if (match.length === 4) {
        // Pattern with timestamp and level
        return {
          timestamp: match[1],
          level: (match[2]?.toUpperCase() === 'WARNING' ? 'WARN' : match[2]?.toUpperCase()) as LogLevel || 'INFO',
          message: match[3] || message,
          originalMessage: message,
          originalEvent: event,
        };
      } else if (match.length === 3) {
        // Pattern with just level
        return {
          level: (match[1]?.toUpperCase() === 'WARNING' ? 'WARN' : match[1]?.toUpperCase()) as LogLevel || 'INFO',
          message: match[2] || message,
          originalMessage: message,
          originalEvent: event,
        };
      }
    }
  }

  // Check event field for error indication
  const level = event.toLowerCase().includes('error') ? 'ERROR' : 
                event.toLowerCase().includes('warn') ? 'WARN' : 'INFO';

  // Handle special cases
  if (message.toLowerCase().includes('error')) {
    return {
      level: 'ERROR',
      message,
      originalMessage: message,
      originalEvent: event,
    };
  }
  
  if (message.toLowerCase().includes('warning') || message.toLowerCase().includes('warn')) {
    return {
      level: 'WARN',
      message,
      originalMessage: message,
      originalEvent: event,
    };
  }

  if (message.toLowerCase().includes('success') || message.toLowerCase().includes('complete')) {
    return {
      level: 'OK',
      message,
      originalMessage: message,
      originalEvent: event,
    };
  }

  return {
    level: level as LogLevel,
    message,
    originalMessage: message,
    originalEvent: event,
  };
}

function LogLevelBadge({ level }: { level: LogLevel }) {
  const config = {
    WARN: { text: 'text-default-warning', bg: 'bg-warning-softest', border: 'border-warning-muted' },
    ERROR: { text: 'text-default-destructive', bg: 'bg-destructive-softest', border: 'border-destructive-muted' },
    DEBUG: { text: 'text-default-info', bg: 'bg-info-softest', border: 'border-info-muted' },
    OK: { text: 'text-default-success', bg: 'bg-success-softest', border: 'border-success-muted' },
    INFO: { text: 'text-muted-foreground', bg: '', border: 'border-border' },
  };

  const style = config[level] || config.INFO;

  return (
    <span className={cn(
      "inline-flex items-center justify-center w-12 text-[10px] font-medium rounded px-1 py-0.5 border",
      style.text,
      style.bg,
      style.border
    )}>
      [{level}]
    </span>
  );
}

export const LogsTabContents = () => {
  const { deploymentId } = useParams();
  const { data: deploymentLogs } = useDeploymentLogsSuspense(
    { deploymentId: deploymentId! },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    },
  );

  const [focus, setFocus] = useState<LogFocus>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [currentLogIndex, setCurrentLogIndex] = useState<number | null>(null);
  const [currentSearchIndex, setCurrentSearchIndex] = useState(0);
  const [copied, setCopied] = useState(false);

  const parsedLogs = useMemo(() => 
    deploymentLogs.events.map(event => parseLogMessage(event.message, event.event)),
    [deploymentLogs.events]
  );

  const logStats = useMemo(() => {
    const stats = { warns: 0, errors: 0 };
    parsedLogs.forEach(log => {
      if (log.level === 'WARN') stats.warns++;
      if (log.level === 'ERROR') stats.errors++;
    });
    return stats;
  }, [parsedLogs]);

  const filteredIndices = useMemo(() => {
    if (focus === 'all' && !searchQuery) return [];
    
    const indices: number[] = [];
    parsedLogs.forEach((log, index) => {
      const matchesFocus = focus === 'all' || 
        (focus === 'warns' && log.level === 'WARN') ||
        (focus === 'errors' && log.level === 'ERROR');
      
      const matchesSearch = !searchQuery || 
        log.message.toLowerCase().includes(searchQuery.toLowerCase());
      
      if (matchesFocus && matchesSearch) {
        indices.push(index);
      }
    });
    
    return indices;
  }, [focus, searchQuery, parsedLogs]);

  const scrollToLog = useCallback((index: number) => {
    const element = document.getElementById(`log-${index}`);
    if (element) {
      element.scrollIntoView({ behavior: 'smooth', block: 'center' });
      setCurrentLogIndex(index);
    }
  }, []);

  const navigateToResult = useCallback((direction: 'next' | 'prev') => {
    if (filteredIndices.length === 0) return;
    
    let newIndex: number;
    if (direction === 'next') {
      newIndex = (currentSearchIndex + 1) % filteredIndices.length;
    } else {
      newIndex = currentSearchIndex === 0 ? filteredIndices.length - 1 : currentSearchIndex - 1;
    }
    
    setCurrentSearchIndex(newIndex);
    scrollToLog(filteredIndices[newIndex]);
  }, [currentSearchIndex, filteredIndices, scrollToLog]);

  const handleFocusChange = (newFocus: LogFocus) => {
    setFocus(newFocus);
    setCurrentSearchIndex(0);
    
    if (newFocus !== 'all') {
      const indices = parsedLogs.map((log, index) => 
        (newFocus === 'warns' && log.level === 'WARN') ||
        (newFocus === 'errors' && log.level === 'ERROR') ? index : -1
      ).filter(i => i !== -1);
      
      if (indices.length > 0) {
        scrollToLog(indices[0]);
      }
    } else {
      setCurrentLogIndex(null);
    }
  };

  const handleSearchChange = (query: string) => {
    setSearchQuery(query);
    setCurrentSearchIndex(0);
    
    if (query) {
      const firstMatch = parsedLogs.findIndex(log => 
        log.message.toLowerCase().includes(query.toLowerCase())
      );
      if (firstMatch !== -1) {
        scrollToLog(firstMatch);
      }
    } else {
      setCurrentLogIndex(null);
    }
  };

  const copyLogs = () => {
    const text = deploymentLogs.events
      .map(event => event.message)
      .join('\n');
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // Keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Cmd/Ctrl + F to focus search
      if ((e.metaKey || e.ctrlKey) && e.key === 'f') {
        e.preventDefault();
        const searchInput = document.querySelector<HTMLInputElement>('[data-search-input]');
        searchInput?.focus();
        return;
      }

      // Cmd/Ctrl + C to copy logs
      if ((e.metaKey || e.ctrlKey) && e.key === 'c' && !window.getSelection()?.toString()) {
        e.preventDefault();
        copyLogs();
        return;
      }

      // Navigation shortcuts (when not in input)
      const isInInput = document.activeElement?.tagName === 'INPUT';
      if (!isInInput) {
        switch(e.key) {
          case '/':
            e.preventDefault();
            const searchInput = document.querySelector<HTMLInputElement>('[data-search-input]');
            searchInput?.focus();
            break;
          case 'n':
            e.preventDefault();
            navigateToResult('next');
            break;
          case 'N':
            if (e.shiftKey) {
              e.preventDefault();
              navigateToResult('prev');
            }
            break;
          case 'j':
            e.preventDefault();
            if (currentLogIndex !== null && currentLogIndex < parsedLogs.length - 1) {
              scrollToLog(currentLogIndex + 1);
            } else if (currentLogIndex === null) {
              scrollToLog(0);
            }
            break;
          case 'k':
            e.preventDefault();
            if (currentLogIndex !== null && currentLogIndex > 0) {
              scrollToLog(currentLogIndex - 1);
            }
            break;
          case 'g':
            if (e.key === 'g' && !e.shiftKey) {
              e.preventDefault();
              scrollToLog(0);
            }
            break;
          case 'G':
            if (e.shiftKey) {
              e.preventDefault();
              scrollToLog(parsedLogs.length - 1);
            }
            break;
          case 'e':
            e.preventDefault();
            handleFocusChange('errors');
            break;
          case 'w':
            e.preventDefault();
            handleFocusChange('warns');
            break;
          case 'a':
            e.preventDefault();
            handleFocusChange('all');
            break;
          case 'Escape':
            setFocus('all');
            setSearchQuery('');
            setCurrentLogIndex(null);
            break;
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [currentLogIndex, parsedLogs.length, navigateToResult, copyLogs]);

  const highlightMatch = (text: string) => {
    if (!searchQuery) return text;
    
    const parts = text.split(new RegExp(`(${searchQuery})`, 'gi'));
    return (
      <>
        {parts.map((part, i) => 
          part.toLowerCase() === searchQuery.toLowerCase() ? (
            <mark key={i} className="bg-yellow-200 dark:bg-yellow-800 text-inherit rounded-sm px-0.5">
              {part}
            </mark>
          ) : part
        )}
      </>
    );
  };

  return (
    <>
      <div className="flex items-center justify-between mb-4">
        <Heading variant="h2" className="text-xl">
          Logs
        </Heading>
        
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1 text-xs">
            <Button 
              size="sm" 
              variant={focus === 'all' ? 'primary' : 'secondary'}
              onClick={() => handleFocusChange('all')}
            >
              <Button.Text>All</Button.Text>
            </Button>
            <Button 
              size="sm" 
              variant={focus === 'warns' ? 'primary' : 'secondary'}
              onClick={() => handleFocusChange('warns')}
              disabled={logStats.warns === 0}
            >
              <Button.Text>Warns ({logStats.warns})</Button.Text>
            </Button>
            <Button 
              size="sm" 
              variant={focus === 'errors' ? 'primary' : 'secondary'}
              onClick={() => handleFocusChange('errors')}
              disabled={logStats.errors === 0}
            >
              <Button.Text>Errors ({logStats.errors})</Button.Text>
            </Button>
          </div>

          <div className="flex items-center gap-1">
            <div className="relative">
              <Input
                data-search-input
                type="text"
                placeholder="Search logs..."
                value={searchQuery}
                onChange={(e) => handleSearchChange(e.target.value)}
                className="w-48 pr-20 text-xs"
              />
              <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1">
                <span className="text-[10px] text-muted-foreground bg-muted px-1 py-0.5 rounded">
                  ⌘F
                </span>
              </div>
            </div>
            
            {filteredIndices.length > 0 && (
              <div className="flex items-center gap-1 border rounded-md">
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => navigateToResult('prev')}
                  className="h-8 px-2"
                >
                  <Icon name="chevron-up" className="size-3" />
                </Button>
                <span className="text-xs text-muted-foreground px-1">
                  {currentSearchIndex + 1}/{filteredIndices.length}
                </span>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => navigateToResult('next')}
                  className="h-8 px-2"
                >
                  <Icon name="chevron-down" className="size-3" />
                </Button>
              </div>
            )}
          </div>

          <Button size="sm" variant="secondary" onClick={copyLogs}>
            {copied ? (
              <>
                <Button.LeftIcon>
                  <CheckIcon className="size-3" />
                </Button.LeftIcon>
                <Button.Text>Copied!</Button.Text>
              </>
            ) : (
              <>
                <Button.LeftIcon>
                  <CopyIcon className="size-3" />
                </Button.LeftIcon>
                <Button.Text>Copy</Button.Text>
              </>
            )}
          </Button>
        </div>
      </div>

      <div className="font-mono w-full overflow-auto bg-surface rounded-lg border border-border">
        {parsedLogs.map((log, index) => {
          const isHighlighted = index === currentLogIndex;
          
          return (
            <div
              id={`log-${index}`}
              key={deploymentLogs.events[index].id}
              className={cn(
                "flex items-start gap-3 px-4 py-2 text-sm border-b border-border/50 last:border-0 transition-colors",
                "hover:bg-muted/30",
                isHighlighted && "bg-warning-softest dark:bg-warning-softest/10 border-l-2 border-l-warning-muted pl-[14px]"
              )}
            >
              <span className="text-muted-foreground select-none min-w-[3ch] text-right">
                {index + 1}
              </span>
              <LogLevelBadge level={log.level} />
              {log.timestamp && (
                <span className="text-muted-foreground text-xs">
                  {log.timestamp}
                </span>
              )}
              <pre className="flex-1 whitespace-pre-wrap break-all">
                {highlightMatch(log.message)}
              </pre>
            </div>
          );
        })}
      </div>

      <div className="mt-2 text-xs text-muted-foreground">
        <details className="cursor-pointer">
          <summary className="hover:text-foreground">Keyboard shortcuts</summary>
          <div className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1">
            <div><kbd className="bg-muted px-1 rounded">⌘F</kbd> or <kbd className="bg-muted px-1 rounded">/</kbd> - Search</div>
            <div><kbd className="bg-muted px-1 rounded">⌘C</kbd> - Copy all logs</div>
            <div><kbd className="bg-muted px-1 rounded">n</kbd> / <kbd className="bg-muted px-1 rounded">⇧N</kbd> - Next/Previous result</div>
            <div><kbd className="bg-muted px-1 rounded">j</kbd> / <kbd className="bg-muted px-1 rounded">k</kbd> - Navigate down/up</div>
            <div><kbd className="bg-muted px-1 rounded">g</kbd> / <kbd className="bg-muted px-1 rounded">⇧G</kbd> - Go to first/last</div>
            <div><kbd className="bg-muted px-1 rounded">e</kbd> / <kbd className="bg-muted px-1 rounded">w</kbd> / <kbd className="bg-muted px-1 rounded">a</kbd> - Errors/Warns/All</div>
            <div><kbd className="bg-muted px-1 rounded">ESC</kbd> - Clear filters</div>
          </div>
        </details>
      </div>
    </>
  );
};

export const AssetsTabContents = () => {
  const { deploymentId } = useParams();
  const { data: deployment } = useDeploymentSuspense(
    { id: deploymentId! },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    },
  );

  const handleDownload = (assetId: string, assetName: string) => {
    const downloadURL = new URL("/rpc/assets.serveOpenAPIv3", getServerURL());
    downloadURL.searchParams.set("id", assetId);
    downloadURL.searchParams.set("project_id", deployment.projectId);

    const link = document.createElement("a");
    link.href = downloadURL.toString();
    link.download = assetName;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  return (
    <>
      <Heading variant="h2" className="mb-4 text-xl">
        Assets
      </Heading>
      <ul className="flex gap-2 flex-wrap">
        {deployment.openapiv3Assets.map((asset) => {
          const downloadURL = new URL(
            "/rpc/assets.serveOpenAPIv3",
            getServerURL(),
          );
          downloadURL.searchParams.set("id", asset.assetId);
          downloadURL.searchParams.set("project_id", deployment.projectId);

          return (
            <li key={asset.id}>
              <MiniCard className="w-64">
                <MiniCard.Title className="truncate max-w-48">
                  <FileCodeIcon size={16} className="inline mr-2" />
                  {asset.name}
                </MiniCard.Title>
                <MiniCard.Description>OpenAPI Document</MiniCard.Description>
                <MiniCard.Actions
                  actions={[
                    {
                      label: "Download",
                      icon: "download",
                      onClick: () => handleDownload(asset.assetId, asset.name),
                    },
                  ]}
                />
              </MiniCard>
            </li>
          );
        })}
      </ul>
    </>
  );
};

export const ToolsTabContents = ({
  deploymentId,
}: {
  deploymentId: string;
}) => {
  return (
    <>
      <Heading variant="h2" className="mb-4 text-xl">
        Tools
      </Heading>
      <ToolsList deploymentId={deploymentId} />
    </>
  );
};
