import {Badge} from "@/components/ui/badge";
import {Sheet, SheetContent} from "@/components/ui/sheet";
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs";
import {HTTPToolLog} from "@gram/client/models/components";
import {CheckIcon, Copy, XIcon} from "lucide-react";
import {formatDuration} from "@/lib/dates";
import {
    formatDetailTimestamp,
    getHttpMethodVariant,
    getSourceFromUrn,
    getToolIcon,
    getToolNameFromUrn,
    isSuccessfulCall,
} from "./utils";

function StatusIcon({isSuccess}: { isSuccess: boolean }) {
    if (isSuccess) {
        return <CheckIcon className="size-4 stroke-success-default"/>;
    }
    return <XIcon className="size-4 stroke-destructive-default"/>
}

interface LogDetailSheetProps {
    log: HTTPToolLog | null;
    open: boolean;
    onOpenChange: (open: boolean) => void;
}

export function LogDetailSheet({log, open, onOpenChange}: LogDetailSheetProps) {
    return (
        <Sheet open={open} onOpenChange={onOpenChange}>
            <SheetContent className="w-[1040px] max-w-[1040px] h-full max-h-screen overflow-y-auto">
                {log && (
                    <div className="flex flex-col gap-8 pt-8 px-6 pb-6">
                        {/* Header */}
                        <div className="flex flex-col gap-6">
                            <div className="flex items-center gap-3">
                                {(() => {
                                    const ToolIcon = getToolIcon(log.toolUrn);
                                    return <ToolIcon className="size-5 shrink-0" strokeWidth={1.5}/>;
                                })()}
                                <h2 className="text-2xl font-light tracking-tight">
                                    {getToolNameFromUrn(log.toolUrn)}
                                </h2>
                                <div className="flex items-center justify-center rounded-full size-6">
                                    <StatusIcon isSuccess={isSuccessfulCall(log)}/>
                                </div>
                            </div>

                            {/* Tabs */}
                            <Tabs defaultValue="request" className="w-full">
                                <TabsList className="w-full">
                                    <TabsTrigger value="request" className="flex-1">
                                        Request
                                    </TabsTrigger>
                                    <TabsTrigger value="response" className="flex-1">
                                        Response
                                    </TabsTrigger>
                                </TabsList>
                                <TabsContent value="request" className="flex flex-col gap-6 mt-6">
                                    {/* Endpoint */}
                                    <div className="flex flex-col gap-3">
                                        <h3 className="text-sm">Endpoint</h3>
                                        <div
                                            className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4 flex items-center gap-3 max-h-[100px] overflow-y-auto border-hidden">
                                            <Badge variant={getHttpMethodVariant(log.httpMethod)}>
                                                {log.httpMethod}
                                            </Badge>
                                            <span className="font-mono text-xs">{log.httpRoute}</span>
                                        </div>
                                    </div>

                                    {/* Request Headers */}
                                    <div className="flex flex-col gap-3">
                                        <div className="flex items-center justify-between">
                                            <h3 className="text-sm">Request Headers</h3>
                                            {log.requestHeaders &&
                                                Object.keys(log.requestHeaders).length > 0 && (
                                                    <button
                                                        className="p-1 rounded hover:bg-surface-secondary-default"
                                                        onClick={() => {
                                                            void navigator.clipboard.writeText(
                                                                JSON.stringify(log.requestHeaders, null, 2)
                                                            );
                                                        }}
                                                    >
                                                        <Copy className="size-4"/>
                                                    </button>
                                                )}
                                        </div>
                                        <div
                                            className="bg-surface-secondary-default border border-neutral-softest flex rounded-lg p-4 max-h-[400px] overflow-y-auto overflow-x-hidden">
                                            {log.requestHeaders &&
                                            Object.keys(log.requestHeaders).length > 0 ? (
                                                <pre className="font-mono text-xs text-default whitespace-pre-wrap">
                          {JSON.stringify(log.requestHeaders, null, 2)}
                        </pre>
                                            ) : (
                                                <div className="text-sm text-muted-foreground">
                                                    No request headers logged
                                                </div>
                                            )}
                                        </div>
                                    </div>
                                </TabsContent>
                                <TabsContent value="response" className="flex flex-col gap-6 mt-6">
                                    {/* Response Headers */}
                                    <div className="flex flex-col gap-3">
                                        <div className="flex items-center justify-between">
                                            <h3 className="text-sm">Response Headers</h3>
                                            {log.responseHeaders &&
                                                Object.keys(log.responseHeaders).length > 0 && (
                                                    <button
                                                        className="p-1 rounded hover:bg-surface-secondary-default"
                                                        onClick={() => {
                                                            void navigator.clipboard.writeText(
                                                                JSON.stringify(log.responseHeaders, null, 2)
                                                            );
                                                        }}
                                                    >
                                                        <Copy className="size-4"/>
                                                    </button>
                                                )}
                                        </div>
                                        <div
                                            className="bg-surface-secondary-default border border-neutral-softest rounded-lg p-4 max-h-[400px] overflow-y-auto overflow-x-hidden">
                                            {log.responseHeaders &&
                                            Object.keys(log.responseHeaders).length > 0 ? (
                                                <pre
                                                    className="font-mono text-xs text-default whitespace-pre-wrap break-all">
                          {JSON.stringify(log.responseHeaders, null, 2)}
                        </pre>
                                            ) : (
                                                <div className="text-sm text-muted-foreground">
                                                    No response headers logged
                                                </div>
                                            )}
                                        </div>
                                    </div>
                                </TabsContent>
                            </Tabs>
                        </div>

                        {/* Properties */}
                        <div className="flex flex-col gap-4 border-t border-neutral-softest pt-4">
                            <h3 className="text-sm">Properties</h3>
                            <div className="flex flex-col gap-4">
                                <div className="flex flex-col gap-1.5">
                                    <div className="text-xs font-mono uppercase text-muted-foreground">
                                        Created
                                    </div>
                                    <div className="text-sm">{formatDetailTimestamp(log.ts)}</div>
                                </div>
                                <div className="flex flex-col gap-1.5">
                                    <div className="text-xs font-mono uppercase text-muted-foreground">
                                        Duration
                                    </div>
                                    <div className="text-sm">{formatDuration(log.durationMs)}</div>
                                </div>
                                <div className="flex flex-col gap-1.5">
                                    <div className="text-xs font-mono uppercase text-muted-foreground">
                                        Source
                                    </div>
                                    <div className="text-sm">{getSourceFromUrn(log.toolUrn)}</div>
                                </div>
                                <div className="flex flex-col gap-1.5">
                                    <div className="text-xs font-mono uppercase text-muted-foreground">
                                        Tool Type
                                    </div>
                                    <div className="flex items-center gap-2">
                                        {(() => {
                                            const ToolIcon = getToolIcon(log.toolUrn);
                                            return <ToolIcon className="size-4 shrink-0" strokeWidth={1.5}/>;
                                        })()}
                                        <span className="text-sm">
                      {log.toolUrn.includes(":http:") ? "OpenAPI" : "Function"}
                    </span>
                                    </div>
                                </div>
                                <div className="flex flex-col gap-1.5">
                                    <div className="text-xs font-mono uppercase text-muted-foreground">
                                        Status
                                    </div>
                                    <div className="flex items-center gap-2">
                                        <StatusIcon isSuccess={isSuccessfulCall(log)}/>
                                        <span className="text-sm">
                      {isSuccessfulCall(log) ? "Success" : "Failed"}
                    </span>
                                    </div>
                                </div>
                                <div className="flex flex-col gap-1.5">
                                    <div className="text-xs font-mono uppercase text-muted-foreground">
                                        Status Code
                                    </div>
                                    <div className="text-sm">{log.statusCode}</div>
                                </div>
                            </div>
                        </div>

                        {/* Actions */}
                        <div className="flex flex-col gap-3 border-t border-neutral-softest pt-4">
                            <h3 className="text-sm">Actions</h3>
                            <div className="flex flex-col gap-2">
                                {log.id && (
                                    <button
                                        className="flex items-center gap-1 text-sm hover:underline"
                                        onClick={() => {
                                            void navigator.clipboard.writeText(log.id!);
                                        }}
                                    >
                                        <Copy className="size-3"/>
                                        <span>Copy log ID</span>
                                    </button>
                                )}
                            </div>
                        </div>
                    </div>
                )}
            </SheetContent>
        </Sheet>
    );
}