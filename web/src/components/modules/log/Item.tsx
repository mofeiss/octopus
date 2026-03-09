'use client';

import { useMemo, useState, useEffect, useRef } from 'react';
import { Clock, Cpu, Zap, AlertCircle, ArrowDownToLine, ArrowUpFromLine, DollarSign, ArrowRight, ArrowDown, Send, MessageSquare, Loader2, RotateCw, ChevronDown, ChevronUp, Pin, User, KeyRound } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { motion, AnimatePresence } from 'motion/react';
import JsonView from '@uiw/react-json-view';
import { githubDarkTheme } from '@uiw/react-json-view/githubDark';
import { githubLightTheme } from '@uiw/react-json-view/githubLight';
import { useTheme } from 'next-themes';
import { type RelayLog, type ChannelAttempt, type LogScope, type ParsedLogContent, useLogDetail } from '@/api/endpoints/log';
import { getModelIcon } from '@/lib/model-icons';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { CopyIconButton } from '@/components/common/CopyButton';
import {
    MorphingDialog,
    MorphingDialogTrigger,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogClose,
    MorphingDialogTitle,
    MorphingDialogDescription,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from '@/components/animate-ui/components/animate/tooltip';

function formatTime(timestamp: number): string {
    const date = new Date(timestamp * 1000);
    return date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    });
}

function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
}

interface RetryBadgeWithTooltipProps {
    channelName: string;
    brandColor: string;
    attempts: ChannelAttempt[];
}

function RetryBadgeWithTooltip({ channelName, brandColor, attempts }: RetryBadgeWithTooltipProps) {
    const t = useTranslations('log.card');

    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Badge
                    variant="secondary"
                    className="shrink-0 text-xs px-1.5 py-0 cursor-help"
                    style={{ backgroundColor: `${brandColor}15`, color: brandColor }}
                >
                    <RotateCw className="size-3 opacity-80" />
                    {attempts.length}<span className="ml-1">{channelName}</span>
                </Badge>
            </TooltipTrigger>
            <TooltipContent className="border bg-card p-2 min-w-[280px] shadow-sm rounded-3xl flex flex-col gap-1">
                {attempts.map((attempt, idx) => (
                    <div key={idx} className="flex flex-col w-full">
                        <div className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted/50 transition-colors">
                            <Badge
                                className={cn(
                                    "h-5 shrink-0 px-1.5 text-[10px] font-bold uppercase shadow-none border-0",
                                    attempt.status === 'success'
                                        ? "bg-primary/15 text-primary"
                                        : "bg-destructive/15 text-destructive"
                                )}
                            >
                                {attempt.status === 'success' ? t('success') : t('failed')}
                            </Badge>
                            <div className="flex min-w-0 flex-col flex-1">
                                <span className="truncate text-xs font-semibold text-foreground">
                                    {attempt.channel_name}
                                </span>
                                <span className="text-[10px] text-muted-foreground">
                                    {attempt.model_name} • {formatDuration(attempt.duration)}
                                </span>
                            </div>
                        </div>
                        {
                            idx < attempts.length - 1 && (
                                <div className="flex justify-center py-0.5">
                                    <ArrowDown className="size-3 text-muted-foreground/30" />
                                </div>
                            )
                        }
                    </div>
                ))}
            </TooltipContent>
        </Tooltip >
    );
}

function DeferredJsonContent({
    content,
    parsedContent,
    fallbackText
}: {
    content: string | undefined;
    parsedContent?: ParsedLogContent;
    fallbackText: string;
}) {
    const { resolvedTheme } = useTheme();
    const { isOpen, isTransitioning } = useMorphingDialog();
    const [parsedState, setParsedState] = useState<{ source: string; isJson: boolean; data: unknown }>({
        source: '',
        isJson: false,
        data: null,
    });
    const hasCachedParsedForCurrent = !!content && !!parsedContent && parsedContent.source === content;

    useEffect(() => {
        if (!isOpen || isTransitioning || !content || hasCachedParsedForCurrent) return;

        let cancelled = false;
        let timeoutId: number | null = null;
        timeoutId = window.setTimeout(() => {
            if (cancelled) return;
            try {
                setParsedState({ source: content, isJson: true, data: JSON.parse(content) });
            } catch {
                setParsedState({ source: content, isJson: false, data: content });
            }
        }, 0);

        return () => {
            cancelled = true;
            if (timeoutId !== null) {
                window.clearTimeout(timeoutId);
            }
        };
    }, [content, hasCachedParsedForCurrent, isOpen, isTransitioning]);

    if (!isOpen) {
        return null;
    }

    if (!content) {
        return (
            <pre className="p-4 text-xs text-muted-foreground whitespace-pre-wrap wrap-break-word leading-relaxed">
                {fallbackText}
            </pre>
        );
    }

    const effectiveParsed = hasCachedParsedForCurrent
        ? parsedContent
        : (parsedState.source === content ? parsedState : null);
    const isLoadingVisual = isTransitioning || !effectiveParsed;

    return (
        <div className="relative h-full">
            <AnimatePresence initial={false} mode="sync">
                {!isLoadingVisual && (
                    effectiveParsed.isJson ? (
                        <motion.div
                            key="deferred-json"
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            transition={{ duration: 0.2 }}
                            className="p-4"
                        >
                            <JsonView
                                value={effectiveParsed.data as object}
                                style={{
                                    ...(resolvedTheme === 'dark' ? githubDarkTheme : githubLightTheme),
                                    fontSize: '12px',
                                    fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
                                    backgroundColor: 'transparent',
                                }}
                                displayDataTypes={false}
                                displayObjectSize={false}
                                collapsed={false}
                            />
                        </motion.div>
                    ) : (
                        <motion.pre
                            key="deferred-text"
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            transition={{ duration: 0.2 }}
                            className="p-4 text-xs text-muted-foreground whitespace-pre-wrap wrap-break-word font-mono leading-relaxed"
                        >
                            {(effectiveParsed.data as string) ?? content}
                        </motion.pre>
                    )
                )}
                {isLoadingVisual && (
                    <motion.div
                        key="deferred-loading"
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.15 }}
                        className="absolute inset-0 p-4 flex items-center justify-center"
                    >
                        <Loader2 className="h-5 w-5 text-muted-foreground animate-spin" />
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function LogContentPanels({ log, scope, onOpenLog }: { log: RelayLog; scope: LogScope; onOpenLog?: (id: number) => void }) {
    const t = useTranslations('log.card');
    const { isOpen } = useMorphingDialog();
    const openStateRef = useRef(false);
    const shouldFetchDetail = isOpen && !!log.content_omitted;
    const detailQuery = useLogDetail({ id: log.id, scope, enabled: shouldFetchDetail });

    const requestContent = detailQuery.data?.request_content ?? log.request_content;
    const responseContent = detailQuery.data?.response_content ?? log.response_content;
    const requestParsedContent = detailQuery.data?.parsed_request_content ?? log.parsed_request_content;
    const responseParsedContent = detailQuery.data?.parsed_response_content ?? log.parsed_response_content;
    const isDetailLoading = shouldFetchDetail && detailQuery.isLoading && !detailQuery.data;
    const isDetailLoadFailed = shouldFetchDetail && !detailQuery.data && !!detailQuery.error;

    useEffect(() => {
        if (isOpen && !openStateRef.current) {
            onOpenLog?.(log.id);
        }
        openStateRef.current = isOpen;
    }, [isOpen, log.id, onOpenLog]);

    return (
        <div className="flex-1 min-h-0 overflow-hidden">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 h-full min-h-0">
                <div className="flex flex-col rounded-2xl border border-border bg-muted/30 overflow-hidden min-h-0">
                    <div className="flex items-center gap-2 px-3 md:px-4 py-2.5 md:py-3 border-b border-border bg-muted/50 shrink-0">
                        <Send className="size-4 text-green-500" />
                        <span className="text-sm font-medium text-card-foreground">{t('requestContent')}</span>
                        <Badge variant="secondary" className="ml-auto text-xs">
                            {log.input_tokens.toLocaleString()} {t('tokens')}
                        </Badge>
                    </div>
                    <div className="flex-1 overflow-auto min-h-0">
                        <AnimatePresence initial={false} mode="sync">
                            {isDetailLoading ? (
                                <motion.div
                                    key={`request-loading-${log.id}`}
                                    initial={{ opacity: 0 }}
                                    animate={{ opacity: 1 }}
                                    exit={{ opacity: 0 }}
                                    transition={{ duration: 0.15 }}
                                    className="h-full p-4 text-xs text-muted-foreground flex items-center justify-center gap-2"
                                >
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                    <span>{t('loadingDetail')}</span>
                                </motion.div>
                            ) : isDetailLoadFailed ? (
                                <motion.pre
                                    key={`request-failed-${log.id}`}
                                    initial={{ opacity: 0 }}
                                    animate={{ opacity: 1 }}
                                    exit={{ opacity: 0 }}
                                    transition={{ duration: 0.2 }}
                                    className="p-4 text-xs text-destructive whitespace-pre-wrap wrap-break-word leading-relaxed"
                                >
                                    {t('detailLoadFailed')}
                                </motion.pre>
                            ) : (
                                <motion.div
                                    key={`request-content-${log.id}`}
                                    initial={{ opacity: 0 }}
                                    animate={{ opacity: 1 }}
                                    exit={{ opacity: 0 }}
                                    transition={{ duration: 0.2 }}
                                    className="h-full"
                                >
                                    <DeferredJsonContent
                                        content={requestContent}
                                        parsedContent={requestParsedContent}
                                        fallbackText={t('noRequestContent')}
                                    />
                                </motion.div>
                            )}
                        </AnimatePresence>
                    </div>
                </div>
                <div className="flex flex-col rounded-2xl border border-border bg-muted/30 overflow-hidden min-h-0">
                    <div className="flex items-center gap-2 px-3 md:px-4 py-2.5 md:py-3 border-b border-border bg-muted/50 shrink-0">
                        <MessageSquare className="size-4 text-purple-500" />
                        <span className="text-sm font-medium text-card-foreground">{t('responseContent')}</span>
                        <Badge variant="secondary" className="ml-auto text-xs">
                            {log.output_tokens.toLocaleString()} {t('tokens')}
                        </Badge>
                    </div>
                    <div className="flex-1 overflow-auto min-h-0">
                        <AnimatePresence initial={false} mode="sync">
                            {isDetailLoading ? (
                                <motion.div
                                    key={`response-loading-${log.id}`}
                                    initial={{ opacity: 0 }}
                                    animate={{ opacity: 1 }}
                                    exit={{ opacity: 0 }}
                                    transition={{ duration: 0.15 }}
                                    className="h-full p-4 text-xs text-muted-foreground flex items-center justify-center gap-2"
                                >
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                    <span>{t('loadingDetail')}</span>
                                </motion.div>
                            ) : isDetailLoadFailed ? (
                                <motion.pre
                                    key={`response-failed-${log.id}`}
                                    initial={{ opacity: 0 }}
                                    animate={{ opacity: 1 }}
                                    exit={{ opacity: 0 }}
                                    transition={{ duration: 0.2 }}
                                    className="p-4 text-xs text-destructive whitespace-pre-wrap wrap-break-word leading-relaxed"
                                >
                                    {t('detailLoadFailed')}
                                </motion.pre>
                            ) : (
                                <motion.div
                                    key={`response-content-${log.id}`}
                                    initial={{ opacity: 0 }}
                                    animate={{ opacity: 1 }}
                                    exit={{ opacity: 0 }}
                                    transition={{ duration: 0.2 }}
                                    className="h-full"
                                >
                                    <DeferredJsonContent
                                        content={responseContent}
                                        parsedContent={responseParsedContent}
                                        fallbackText={t('noResponseContent')}
                                    />
                                </motion.div>
                            )}
                        </AnimatePresence>
                    </div>
                </div>
            </div>
        </div>
    );
}

export function LogCard({ log, scope = 'admin', onOpenLog }: { log: RelayLog; scope?: LogScope; onOpenLog?: (id: number) => void }) {
    const t = useTranslations('log.card');
    const { Avatar: ModelAvatar, color: brandColor } = useMemo(
        () => getModelIcon(log.actual_model_name),
        [log.actual_model_name]
    );

    // [fork] 过滤掉被手动禁用的渠道记录
    const filteredAttempts = useMemo(() => {
        if (!log.attempts) return [];
        return log.attempts.filter(attempt => attempt.msg !== 'group item disabled');
    }, [log.attempts]);

    const hasError = !!log.error;
    const hasMultipleAttempts = filteredAttempts.length > 1;
    const [isDiagnosticExpanded, setIsDiagnosticExpanded] = useState(true);

    return (
        <TooltipProvider>
            <MorphingDialog>
                <MorphingDialogTrigger
                    className={cn(
                        "rounded-3xl border bg-card custom-shadow w-full text-left",
                        "hover:shadow-md transition-shadow duration-200",
                        hasError ? "border-destructive/40" : "border-border",
                    )}
                >
                    <div className={cn("p-4 grid grid-cols-[auto_1fr] gap-4", hasError ? "items-start" : "items-center")}>
                        <ModelAvatar size={40} />
                        <div className="min-w-0 flex flex-col gap-3">
                            <div className="flex items-center gap-2 min-w-0 text-sm">
                                <span className="font-semibold text-card-foreground truncate" title={log.request_model_name}>
                                    {log.request_model_name}
                                </span>
                                <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/50" />
                                {hasMultipleAttempts ? (
                                    <RetryBadgeWithTooltip
                                        channelName={log.channel_name}
                                        brandColor={brandColor}
                                        attempts={filteredAttempts}
                                    />
                                ) : (
                                    <Badge
                                        variant="secondary"
                                        className="shrink-0 text-xs px-1.5 py-0"
                                        style={{ backgroundColor: `${brandColor}15`, color: brandColor }}
                                    >
                                        {log.channel_name}
                                    </Badge>
                                )}
                                <span className="text-muted-foreground truncate" title={log.actual_model_name}>
                                    {log.actual_model_name}
                                </span>
                                {/* [fork] channel key info */}
                                {log.channel_key_preview && (
                                    <>
                                        <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/50" />
                                        <Badge
                                            variant="secondary"
                                            className="shrink-0 text-xs px-1.5 py-0 inline-flex items-center gap-0.5"
                                            style={{ backgroundColor: `${brandColor}15`, color: brandColor }}
                                        >
                                            <KeyRound className="size-3" />
                                            {log.channel_key_index}
                                        </Badge>
                                        {log.channel_key_remark && (
                                            <Badge variant="secondary" className="shrink-0 text-xs px-1.5 py-0">
                                                {log.channel_key_remark}
                                            </Badge>
                                        )}
                                        <span className="text-muted-foreground truncate" title={log.channel_key_preview}>
                                            {log.channel_key_preview}
                                        </span>
                                    </>
                                )}
                                {filteredAttempts.some(a => a.sticky) && (
                                    <Pin className="size-3.5 shrink-0 text-amber-500" />
                                )}
                            </div>
                            <div className="grid grid-cols-2 md:grid-cols-7 gap-x-4 gap-y-2 text-xs tabular-nums text-muted-foreground">
                                <div className="flex items-center gap-1.5">
                                    <Clock className="size-3.5 shrink-0" style={{ color: brandColor }} />
                                    <span>{formatTime(log.time)}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <Zap className="size-3.5 shrink-0 text-amber-500" />
                                    <span>{t('firstToken')} {formatDuration(log.ftut)}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <Cpu className="size-3.5 shrink-0 text-blue-500" />
                                    <span>{t('totalTime')} {formatDuration(log.use_time)}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <ArrowDownToLine className="size-3.5 shrink-0 text-green-500" />
                                    <span>{t('input')} {log.input_tokens.toLocaleString()}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <ArrowUpFromLine className="size-3.5 shrink-0 text-purple-500" />
                                    <span>{t('output')} {log.output_tokens.toLocaleString()}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <DollarSign className="size-3.5 shrink-0 text-emerald-500" />
                                    <span className="font-medium text-emerald-600 dark:text-emerald-400">
                                        {t('cost')} {Number(log.cost).toFixed(6)}
                                    </span>
                                </div>
                                {/* [fork] user apikey name */}
                                {log.api_key_name && (
                                    <div className="flex items-center gap-1.5">
                                        <User className="size-3.5 shrink-0 text-sky-500" />
                                        <span>{t('user')} {log.api_key_name}</span>
                                    </div>
                                )}
                            </div>
                            {hasError && (
                                <div className="p-2.5 rounded-xl bg-destructive/10 border border-destructive/20 overflow-hidden">
                                    <p className="text-xs text-destructive line-clamp-2">{log.error}</p>
                                </div>
                            )}
                        </div>
                    </div>
                </MorphingDialogTrigger>

                <MorphingDialogContainer>
                    <MorphingDialogContent className="relative w-[calc(100vw-2rem)] md:w-[80vw] bg-card text-card-foreground px-6 py-4 rounded-3xl custom-shadow h-[calc(100vh-2rem)] flex flex-col overflow-hidden">
                        <MorphingDialogClose className="top-4 right-5 text-muted-foreground hover:text-foreground transition-colors" />
                        <MorphingDialogTitle className="flex items-center gap-2 mb-3 text-sm">
                            <ModelAvatar size={28} />
                            <span className="font-semibold text-card-foreground">{log.request_model_name}</span>
                            <ArrowRight className="size-3.5 text-muted-foreground/50" />
                            {hasMultipleAttempts ? (
                                <RetryBadgeWithTooltip
                                    channelName={log.channel_name}
                                    brandColor={brandColor}
                                    attempts={filteredAttempts}
                                />
                            ) : (
                                <Badge
                                    variant="secondary"
                                    className="text-xs px-1.5 py-0"
                                    style={{ backgroundColor: `${brandColor}15`, color: brandColor }}
                                >
                                    {log.channel_name}
                                </Badge>
                            )}
                            <span className="text-muted-foreground">{log.actual_model_name}</span>
                            {/* [fork] channel key info */}
                            {log.channel_key_preview && (
                                <>
                                    <ArrowRight className="size-3.5 text-muted-foreground/50" />
                                    <Badge
                                        variant="secondary"
                                        className="shrink-0 text-xs px-1.5 py-0 inline-flex items-center gap-0.5"
                                        style={{ backgroundColor: `${brandColor}15`, color: brandColor }}
                                    >
                                        <KeyRound className="size-3" />
                                        {log.channel_key_index}
                                    </Badge>
                                    {log.channel_key_remark && (
                                        <Badge variant="secondary" className="shrink-0 text-xs px-1.5 py-0">
                                            {log.channel_key_remark}
                                        </Badge>
                                    )}
                                    <span className="text-muted-foreground truncate" title={log.channel_key_preview}>
                                        {log.channel_key_preview}
                                    </span>
                                </>
                            )}
                            {filteredAttempts.some(a => a.sticky) && (
                                <Pin className="size-3.5 shrink-0 text-amber-500" />
                            )}
                        </MorphingDialogTitle>

                        <MorphingDialogDescription className="flex-1 min-h-0">
                            <div className="flex flex-col min-h-0 h-full gap-4">
                                {(hasError || hasMultipleAttempts) && (
                                    <div className={cn(
                                        "flex-initial min-h-0 flex flex-col rounded-2xl border overflow-hidden max-h-[40%]",
                                        hasError
                                            ? "bg-destructive/5 border-destructive/20"
                                            : "bg-secondary/30 border-border/50"
                                    )}>
                                        <div
                                            className={cn(
                                                "flex items-center gap-2 px-3 py-2.5 shrink-0 cursor-pointer select-none hover:bg-muted/50 transition-colors",
                                                hasError && "hover:bg-destructive/10"
                                            )}
                                            onClick={() => setIsDiagnosticExpanded(!isDiagnosticExpanded)}
                                        >
                                            {hasError ? (
                                                <AlertCircle className="size-4 text-destructive" />
                                            ) : (
                                                <RotateCw className="size-4 text-muted-foreground" />
                                            )}
                                            <span className={cn(
                                                "text-sm font-medium",
                                                hasError ? "text-destructive" : "text-secondary-foreground"
                                            )}>
                                                {hasError ? t('errorInfo') : t('retryDetails')}
                                            </span>
                                            <div className="ml-auto flex items-center gap-2">
                                                {hasMultipleAttempts && (
                                                    <Badge
                                                        variant="outline"
                                                        className={cn(
                                                            "text-xs border-0",
                                                            hasError
                                                                ? "bg-destructive/10 text-destructive"
                                                                : "bg-secondary text-secondary-foreground"
                                                        )}
                                                    >
                                                        {filteredAttempts.length} {t('attempts')}
                                                    </Badge>
                                                )}
                                                {isDiagnosticExpanded ? (
                                                    <ChevronUp className="size-4 text-muted-foreground" />
                                                ) : (
                                                    <ChevronDown className="size-4 text-muted-foreground" />
                                                )}
                                            </div>
                                        </div>

                                        <AnimatePresence initial={false}>
                                            {isDiagnosticExpanded && (
                                                <motion.div
                                                    initial={{ height: 0, opacity: 0 }}
                                                    animate={{ height: "auto", opacity: 1 }}
                                                    exit={{ height: 0, opacity: 0 }}
                                                    transition={{ duration: 0.2, ease: "easeInOut" }}
                                                    className="overflow-hidden flex flex-col min-h-0"
                                                >
                                                    <div className="flex-1 overflow-auto p-2.5 md:p-3 flex flex-col gap-4">
                                                        {hasError && (
                                                            <div className="relative pl-1">
                                                                <div className="absolute right-0 top-0">
                                                                    <CopyIconButton
                                                                        text={log.error ?? ''}
                                                                        className="p-1 rounded-md text-destructive/60 hover:text-destructive hover:bg-destructive/10 transition-colors"
                                                                        copyIconClassName="size-4"
                                                                        checkIconClassName="size-4"
                                                                    />
                                                                </div>
                                                                <p className="text-sm text-destructive whitespace-pre-wrap wrap-break-word pr-8 leading-relaxed">
                                                                    {log.error}
                                                                </p>
                                                            </div>
                                                        )}

                                                        {hasMultipleAttempts && (
                                                            <div className="flex flex-col gap-2">
                                                                {filteredAttempts.map((attempt, idx) => (
                                                                    <div
                                                                        key={idx}
                                                                        className={cn(
                                                                            "text-xs p-2.5 rounded-xl border transition-colors flex flex-col gap-2",
                                                                            attempt.status === 'success'
                                                                                ? "bg-primary/5 border-primary/20 hover:bg-primary/10"
                                                                                : "bg-destructive/5 border-destructive/20 hover:bg-destructive/10"
                                                                        )}
                                                                    >
                                                                        <div className="flex items-center gap-2">
                                                                            <span className="font-semibold text-foreground">
                                                                                {attempt.channel_name}
                                                                            </span>
                                                                            <span className="text-muted-foreground">
                                                                                ({attempt.model_name})
                                                                            </span>
                                                                            {/* [fork] attempt channel key info */}
                                                                            {attempt.channel_key_preview && (
                                                                                <>
                                                                                    <Badge
                                                                                        variant="secondary"
                                                                                        className="shrink-0 text-[10px] px-1 py-0 inline-flex items-center gap-0.5"
                                                                                        style={{ backgroundColor: `${brandColor}15`, color: brandColor }}
                                                                                    >
                                                                                        <KeyRound className="size-2.5" />
                                                                                        {attempt.channel_key_index}
                                                                                    </Badge>
                                                                                    {attempt.channel_key_remark && (
                                                                                        <Badge variant="secondary" className="shrink-0 text-[10px] px-1 py-0">
                                                                                            {attempt.channel_key_remark}
                                                                                        </Badge>
                                                                                    )}
                                                                                    <span className="text-muted-foreground truncate">
                                                                                        {attempt.channel_key_preview}
                                                                                    </span>
                                                                                </>
                                                                            )}
                                                                            <span className="ml-auto text-muted-foreground tabular-nums font-mono">
                                                                                {formatDuration(attempt.duration)}
                                                                            </span>
                                                                        </div>
                                                                        {attempt.msg && (
                                                                            <div className="text-destructive/90 pl-2 border-l-2 border-destructive/30 text-[11px] leading-relaxed">
                                                                                {attempt.msg}
                                                                            </div>
                                                                        )}
                                                                    </div>
                                                                ))}
                                                            </div>
                                                        )}
                                                    </div>
                                                </motion.div>
                                            )}
                                        </AnimatePresence>
                                    </div>
                                )}
                                <LogContentPanels log={log} scope={scope} onOpenLog={onOpenLog} />
                            </div>
                        </MorphingDialogDescription>

                        <div className="flex flex-wrap items-center gap-3 md:gap-4 pt-4 mt-auto text-xs text-muted-foreground shrink-0">
                            <div className="flex items-center gap-1.5">
                                <Clock className="size-3.5" style={{ color: brandColor }} />
                                <span className="tabular-nums">{formatTime(log.time)}</span>
                            </div>
                            <div className="flex items-center gap-1.5">
                                <Zap className="size-3.5 text-amber-500" />
                                <span>{t('firstTokenTime')}: {formatDuration(log.ftut)}</span>
                            </div>
                            <div className="flex items-center gap-1.5">
                                <Cpu className="size-3.5 text-blue-500" />
                                <span>{t('totalTime')}: {formatDuration(log.use_time)}</span>
                            </div>
                            <div className="flex items-center gap-1.5">
                                <DollarSign className="size-3.5 text-emerald-500" />
                                <span className="font-medium text-emerald-600 dark:text-emerald-400">
                                    {t('cost')}: {Number(log.cost).toFixed(6)}
                                </span>
                            </div>
                        </div>
                    </MorphingDialogContent>
                </MorphingDialogContainer>
            </MorphingDialog>
        </TooltipProvider>
    );
}
