'use client';

import { useMemo, useState, useEffect, useRef, type ReactNode } from 'react';
import { Clock, Cpu, Zap, AlertCircle, ArrowDownToLine, ArrowUpFromLine, DollarSign, ArrowRight, ArrowDown, Send, MessageSquare, Loader2, RotateCw, ChevronDown, ChevronUp, Pin, User, KeyRound, Braces, Workflow } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { motion, AnimatePresence } from 'motion/react';
import JsonView from '@uiw/react-json-view';
import { githubDarkTheme } from '@uiw/react-json-view/githubDark';
import { githubLightTheme } from '@uiw/react-json-view/githubLight';
import { useTheme } from 'next-themes';
import { type RelayLog, type ChannelAttempt, type LogScope, type ParsedLogContent, useLogDetail } from '@/api/endpoints/log';
import { getModelIcon } from '@/lib/model-icons';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { CopyIconButton } from '@/components/common/CopyButton';
import { type LogDetailTabKey, type LogRequestSectionKey, type LogResponseSectionKey, type LogVisualSourceMode, useLogDetailStore } from './detail-store';
import { MessageFlowExpandedContentOverlay, MessageFlowVisualizer } from './MessageFlowVisualizer';
import { detectMessageFlowProtocol, normalizeLogProtocol, parseLogMessageFlow, type MessageFlowItem } from './message-flow-parser';
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

type LogContentSectionKey = LogRequestSectionKey | LogResponseSectionKey;

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

function attemptTone(status: ChannelAttempt['status']) {
    if (status === 'success') return 'success';
    if (status === 'client_canceled' || status === 'skipped' || status === 'circuit_break') return 'neutral';
    return 'failed';
}

function attemptStatusLabel(status: ChannelAttempt['status'], t: ReturnType<typeof useTranslations>) {
    if (status === 'success') return t('success');
    if (status === 'client_canceled') return t('clientCanceled');
    if (status === 'skipped') return t('skipped');
    if (status === 'circuit_break') return t('circuitBreak');
    return t('failed');
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
                                    attemptTone(attempt.status) === 'success'
                                        ? "bg-primary/15 text-primary"
                                        : attemptTone(attempt.status) === 'neutral'
                                            ? "bg-secondary text-secondary-foreground"
                                            : "bg-destructive/15 text-destructive"
                                )}
                            >
                                {attemptStatusLabel(attempt.status, t)}
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

function buildProtocolSectionTitle(prefix: string, protocol: string | undefined): string {
    const normalized = protocol?.trim();
    if (!normalized) return prefix;
    return `${prefix}(${normalized})`;
}

// [fork] Keep the high-level detail pages mounted so tab hot switching preserves panel state.
function DetailTabSwitch() {
    const t = useTranslations('log.card');
    const activeDetailTab = useLogDetailStore((state) => state.activeDetailTab);
    const setActiveDetailTab = useLogDetailStore((state) => state.setActiveDetailTab);
    const tabs: Array<{ value: LogDetailTabKey; label: string; icon: ReactNode }> = [
        { value: 'json_sse', label: t('jsonSseTab'), icon: <Braces className="size-3.5" /> },
        { value: 'visualization', label: t('visualizationTab'), icon: <Workflow className="size-3.5" /> },
    ];

    return (
        <div className="absolute left-1/2 top-3 z-10 -translate-x-1/2">
            <div className="inline-flex rounded-lg border border-border bg-background/95 p-0.5 shadow-sm backdrop-blur">
                {tabs.map((tab) => (
                    <Button
                        key={tab.value}
                        type="button"
                        variant={activeDetailTab === tab.value ? 'secondary' : 'ghost'}
                        size="sm"
                        className={cn(
                            'h-7 rounded-md px-2 text-xs md:px-2.5',
                            activeDetailTab !== tab.value && 'text-muted-foreground hover:text-foreground'
                        )}
                        onClick={(event) => {
                            event.stopPropagation();
                            setActiveDetailTab(tab.value);
                        }}
                    >
                        {tab.icon}
                        <span>{tab.label}</span>
                    </Button>
                ))}
            </div>
        </div>
    );
}

function LogContentSection({
    sectionValue,
    value,
    icon,
    title,
    parsedContent,
    fallbackText,
    isActive,
    onActivate,
}: {
    sectionValue: LogContentSectionKey;
    value: string | undefined;
    icon: ReactNode;
    title: string;
    parsedContent?: ParsedLogContent;
    fallbackText: string;
    isActive: boolean;
    onActivate: (value: LogContentSectionKey) => void;
}) {
    return (
        <motion.div
            initial={false}
            animate={{ flexGrow: isActive ? 1 : 0 }}
            transition={{ duration: 0.22, ease: 'easeInOut' }}
            className={cn(
                'flex min-h-0 flex-col overflow-hidden rounded-xl border border-border/80 bg-background/70 transition-colors',
                isActive ? 'flex-1' : 'shrink-0'
            )}
        >
            <button
                type="button"
                className={cn(
                    'flex items-center justify-between gap-3 px-3 md:px-4 py-3 text-left text-sm font-medium text-card-foreground transition-colors',
                    isActive ? 'border-b border-border/80 bg-muted/35' : 'hover:bg-muted/30'
                )}
                aria-expanded={isActive}
                onClick={() => onActivate(sectionValue)}
            >
                <div className="flex min-w-0 items-center gap-2 text-left">
                    {icon}
                    <span className="truncate">{title}</span>
                </div>
                {isActive ? (
                    <ChevronUp className="size-4 shrink-0 text-muted-foreground" />
                ) : (
                    <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
                )}
            </button>
            <div className={cn('min-h-0 overflow-hidden', isActive ? 'flex-1' : 'h-0')}>
                <div className="h-full min-h-0 overflow-auto">
                    <DeferredJsonContent
                        content={value}
                        parsedContent={parsedContent}
                        fallbackText={fallbackText}
                    />
                </div>
            </div>
        </motion.div>
    );
}

function RequestContentPanel({
    inputTokens,
    isDetailLoading,
    isDetailLoadFailed,
    originalRequestContent,
    originalRequestParsedContent,
    originalRequestTitle,
    outboundRequestContent,
    outboundRequestParsedContent,
    outboundRequestTitle,
}: {
    inputTokens: number;
    isDetailLoading: boolean;
    isDetailLoadFailed: boolean;
    originalRequestContent: string | undefined;
    originalRequestParsedContent?: ParsedLogContent;
    originalRequestTitle: string;
    outboundRequestContent: string | undefined;
    outboundRequestParsedContent?: ParsedLogContent;
    outboundRequestTitle: string;
}) {
    const t = useTranslations('log.card');
    const activeRequestSection = useLogDetailStore((state) => state.activeRequestSection);
    const setActiveRequestSection = useLogDetailStore((state) => state.setActiveRequestSection);

    const handleActivate = (sectionValue: LogContentSectionKey) => {
        const nextSection = activeRequestSection === sectionValue ? null : sectionValue;
        setActiveRequestSection(nextSection as LogRequestSectionKey | null);
    };

    return (
        <div className="flex flex-col rounded-2xl border border-border bg-muted/30 overflow-hidden min-h-0">
            <div className="flex items-center gap-2 px-3 md:px-4 py-2.5 md:py-3 border-b border-border bg-muted/50 shrink-0">
                <Send className="size-4 text-green-500" />
                <span className="text-sm font-medium text-card-foreground">{t('requestContent')}</span>
                <Badge variant="secondary" className="ml-auto text-xs">
                    {inputTokens.toLocaleString()} {t('tokens')}
                </Badge>
            </div>
            <div className="flex-1 min-h-0 overflow-hidden">
                <AnimatePresence initial={false} mode="sync">
                    {isDetailLoading ? (
                        <motion.div
                            key="request-loading"
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
                            key="request-failed"
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
                            key="request-content"
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            transition={{ duration: 0.2 }}
                            className="h-full p-3 md:p-4"
                        >
                            <div className="flex h-full min-h-0 flex-col gap-3 overflow-hidden">
                                <LogContentSection
                                    sectionValue="original"
                                    value={originalRequestContent}
                                    parsedContent={originalRequestParsedContent}
                                    title={originalRequestTitle}
                                    fallbackText={t('noRequestContent')}
                                    isActive={activeRequestSection === 'original'}
                                    onActivate={handleActivate}
                                    icon={<ArrowDownToLine className="size-4 text-sky-500 shrink-0" />}
                                />
                                <LogContentSection
                                    sectionValue="outbound"
                                    value={outboundRequestContent}
                                    parsedContent={outboundRequestParsedContent}
                                    title={outboundRequestTitle}
                                    fallbackText={t('noRequestContent')}
                                    isActive={activeRequestSection === 'outbound'}
                                    onActivate={handleActivate}
                                    icon={<ArrowUpFromLine className="size-4 text-emerald-500 shrink-0" />}
                                />
                            </div>
                        </motion.div>
                    )}
                </AnimatePresence>
            </div>
        </div>
    );
}

function ResponseContentPanel({
    outputTokens,
    isDetailLoading,
    isDetailLoadFailed,
    originalResponseContent,
    originalResponseParsedContent,
    streamPreviewContent,
    streamPreviewParsedContent,
    responseContent,
    responseParsedContent,
}: {
    outputTokens: number;
    isDetailLoading: boolean;
    isDetailLoadFailed: boolean;
    originalResponseContent: string | undefined;
    originalResponseParsedContent?: ParsedLogContent;
    streamPreviewContent: string | undefined;
    streamPreviewParsedContent?: ParsedLogContent;
    responseContent: string | undefined;
    responseParsedContent?: ParsedLogContent;
}) {
    const t = useTranslations('log.card');
    const activeResponseSection = useLogDetailStore((state) => state.activeResponseSection);
    const setActiveResponseSection = useLogDetailStore((state) => state.setActiveResponseSection);
    const hasStreamPreview = !!streamPreviewContent;

    const handleActivate = (sectionValue: LogContentSectionKey) => {
        const nextSection = activeResponseSection === sectionValue ? null : sectionValue;
        setActiveResponseSection(nextSection);
    };

    useEffect(() => {
        if (!hasStreamPreview && activeResponseSection === 'preview') {
            setActiveResponseSection('original');
        }
    }, [activeResponseSection, hasStreamPreview, setActiveResponseSection]);

    return (
        <div className="flex flex-col rounded-2xl border border-border bg-muted/30 overflow-hidden min-h-0">
            <div className="flex items-center gap-2 px-3 md:px-4 py-2.5 md:py-3 border-b border-border bg-muted/50 shrink-0">
                <MessageSquare className="size-4 text-purple-500" />
                <span className="text-sm font-medium text-card-foreground">{t('responseContent')}</span>
                <Badge variant="secondary" className="ml-auto text-xs">
                    {outputTokens.toLocaleString()} {t('tokens')}
                </Badge>
            </div>
            <div className="flex-1 min-h-0 overflow-hidden">
                <AnimatePresence initial={false} mode="sync">
                    {isDetailLoading ? (
                        <motion.div
                            key="response-loading"
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
                            key="response-failed"
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
                            key="response-content"
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            transition={{ duration: 0.2 }}
                            className="h-full p-3 md:p-4"
                        >
                            <div className="flex h-full min-h-0 flex-col gap-3 overflow-hidden">
                                <LogContentSection
                                    sectionValue="original"
                                    value={originalResponseContent}
                                    parsedContent={originalResponseParsedContent}
                                    title={t('originalResponseLabel')}
                                    fallbackText={t('noResponseContent')}
                                    isActive={activeResponseSection === 'original'}
                                    onActivate={handleActivate}
                                    icon={<ArrowDownToLine className="size-4 text-sky-500 shrink-0" />}
                                />
                                {hasStreamPreview && (
                                    <LogContentSection
                                        sectionValue="preview"
                                        value={streamPreviewContent}
                                        parsedContent={streamPreviewParsedContent}
                                        title={t('streamPreviewLabel')}
                                        fallbackText={t('noResponseContent')}
                                        isActive={activeResponseSection === 'preview'}
                                        onActivate={handleActivate}
                                        icon={<Braces className="size-4 text-amber-500 shrink-0" />}
                                    />
                                )}
                                <LogContentSection
                                    sectionValue="outbound"
                                    value={responseContent}
                                    parsedContent={responseParsedContent}
                                    title={t('outboundResponseLabel')}
                                    fallbackText={t('noResponseContent')}
                                    isActive={activeResponseSection === 'outbound'}
                                    onActivate={handleActivate}
                                    icon={<ArrowUpFromLine className="size-4 text-emerald-500 shrink-0" />}
                                />
                            </div>
                        </motion.div>
                    )}
                </AnimatePresence>
            </div>
        </div>
    );
}

interface LogDetailContentData {
    originalRequestContent: string | undefined;
    outboundRequestContent: string | undefined;
    originalResponseContent: string | undefined;
    streamPreviewContent: string | undefined;
    responseContent: string | undefined;
    originalRequestProtocol: string | undefined;
    outboundRequestProtocol: string | undefined;
    originalRequestParsedContent?: ParsedLogContent;
    outboundRequestParsedContent?: ParsedLogContent;
    originalResponseParsedContent?: ParsedLogContent;
    streamPreviewParsedContent?: ParsedLogContent;
    responseParsedContent?: ParsedLogContent;
    isDetailLoading: boolean;
    isDetailLoadFailed: boolean;
}

function JsonSseLogDetailView({ log, data }: { log: RelayLog; data: LogDetailContentData }) {
    const t = useTranslations('log.card');
    const originalRequestTitle = buildProtocolSectionTitle(t('originalRequestLabel'), data.originalRequestProtocol);
    const outboundRequestTitle = buildProtocolSectionTitle(t('outboundRequestLabel'), data.outboundRequestProtocol);

    return (
        <div className="h-full min-h-0 overflow-hidden">
            <div className="grid h-full min-h-0 grid-cols-1 gap-4 md:grid-cols-2">
                <RequestContentPanel
                    key={`request-panel-${log.id}`}
                    inputTokens={log.input_tokens}
                    isDetailLoading={data.isDetailLoading}
                    isDetailLoadFailed={data.isDetailLoadFailed}
                    originalRequestContent={data.originalRequestContent}
                    originalRequestParsedContent={data.originalRequestParsedContent}
                    originalRequestTitle={originalRequestTitle}
                    outboundRequestContent={data.outboundRequestContent}
                    outboundRequestParsedContent={data.outboundRequestParsedContent}
                    outboundRequestTitle={outboundRequestTitle}
                />
                <ResponseContentPanel
                    outputTokens={log.output_tokens}
                    isDetailLoading={data.isDetailLoading}
                    isDetailLoadFailed={data.isDetailLoadFailed}
                    originalResponseContent={data.originalResponseContent}
                    originalResponseParsedContent={data.originalResponseParsedContent}
                    streamPreviewContent={data.streamPreviewContent}
                    streamPreviewParsedContent={data.streamPreviewParsedContent}
                    responseContent={data.responseContent}
                    responseParsedContent={data.responseParsedContent}
                />
            </div>
        </div>
    );
}

function protocolsMatch(data: LogDetailContentData): boolean {
    const original = normalizeLogProtocol(data.originalRequestProtocol)
        || detectMessageFlowProtocol(data.originalRequestContent, 'request');
    const outbound = normalizeLogProtocol(data.outboundRequestProtocol)
        || detectMessageFlowProtocol(data.outboundRequestContent, 'request');
    if (!original || !outbound || original === 'unsupported' || outbound === 'unsupported') return true;
    return original === outbound;
}

function VisualSourceSwitch({
    disabled,
}: {
    disabled: boolean;
}) {
    const t = useTranslations('log.card.visual');
    const visualSourceMode = useLogDetailStore((state) => state.visualSourceMode);
    const setVisualSourceMode = useLogDetailStore((state) => state.setVisualSourceMode);
    const modes: Array<{ value: LogVisualSourceMode; label: string }> = [
        { value: 'raw', label: t('rawSource') },
        { value: 'converted', label: t('convertedSource') },
    ];

    useEffect(() => {
        if (disabled && visualSourceMode !== 'raw') {
            setVisualSourceMode('raw');
        }
    }, [disabled, setVisualSourceMode, visualSourceMode]);

    if (disabled) return null;

    return (
        <div className="inline-flex rounded-xl border border-border bg-background p-1">
            {modes.map((mode) => (
                <Button
                    key={mode.value}
                    type="button"
                    variant={visualSourceMode === mode.value ? 'secondary' : 'ghost'}
                    size="sm"
                    className={cn(
                        'h-8 rounded-lg px-3 text-xs',
                        visualSourceMode !== mode.value && 'text-muted-foreground hover:text-foreground'
                    )}
                    onClick={() => setVisualSourceMode(mode.value)}
                >
                    {mode.label}
                </Button>
            ))}
        </div>
    );
}

function VisualMessageFlowPanel({ data }: { data: LogDetailContentData }) {
    const t = useTranslations('log.card.visual');
    const visualSourceMode = useLogDetailStore((state) => state.visualSourceMode);
    const visualContentMode = useLogDetailStore((state) => state.visualContentMode);
    const setVisualContentMode = useLogDetailStore((state) => state.setVisualContentMode);
    const [expandedContent, setExpandedContent] = useState<{ resultKey: string; itemId: string } | null>(null);
    const expandedOverlayRef = useRef<HTMLDivElement | null>(null);
    const sameProtocol = protocolsMatch(data);
    const effectiveMode: LogVisualSourceMode = sameProtocol ? 'raw' : visualSourceMode;
    const parseResult = useMemo(() => {
        if (effectiveMode === 'converted') {
            return parseLogMessageFlow({
                sourceMode: 'converted',
                requestProtocol: data.outboundRequestProtocol,
                responseProtocol: data.originalRequestProtocol,
                requestContent: data.outboundRequestContent,
                responseContent: data.responseContent,
                responseFallbackContent: data.streamPreviewContent,
            });
        }

        return parseLogMessageFlow({
            sourceMode: 'raw',
            requestProtocol: data.originalRequestProtocol,
            responseProtocol: data.outboundRequestProtocol,
            requestContent: data.originalRequestContent,
            responseContent: data.originalResponseContent,
        });
    }, [
        data.originalRequestContent,
        data.originalRequestProtocol,
        data.originalResponseContent,
        data.outboundRequestContent,
        data.outboundRequestProtocol,
        data.responseContent,
        data.streamPreviewContent,
        effectiveMode,
    ]);
    const resultKey = useMemo(() => (
        `${parseResult.sourceMode}:${parseResult.protocolPair.request}:${parseResult.protocolPair.response}:${parseResult.items.map((item) => item.id).join('|')}`
    ), [parseResult.items, parseResult.protocolPair.request, parseResult.protocolPair.response, parseResult.sourceMode]);
    const expandedItem = expandedContent?.resultKey === resultKey
        ? parseResult.items.find((item) => item.id === expandedContent.itemId)
        : undefined;
    const handleExpandItem = (item: MessageFlowItem) => {
        setExpandedContent({ resultKey, itemId: item.id });
    };

    useEffect(() => {
        if (!expandedItem) return;

        const handlePointerDown = (event: PointerEvent) => {
            const target = event.target;
            if (target instanceof Node && expandedOverlayRef.current?.contains(target)) return;
            setExpandedContent(null);
        };

        document.addEventListener('pointerdown', handlePointerDown, true);
        return () => document.removeEventListener('pointerdown', handlePointerDown, true);
    }, [expandedItem]);

    if (data.isDetailLoading) {
        return (
            <div className="flex h-full items-center justify-center gap-2 rounded-2xl border border-border bg-muted/30 text-xs text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                <span>{t('loading')}</span>
            </div>
        );
    }

    if (data.isDetailLoadFailed) {
        return (
            <div className="flex h-full items-center justify-center rounded-2xl border border-destructive/20 bg-destructive/5 p-6 text-sm text-destructive">
                {t('detailLoadFailed')}
            </div>
        );
    }

    return (
        <div className="relative flex h-full min-h-0 flex-col overflow-hidden rounded-2xl border border-border bg-muted/20 p-3 md:p-4">
            <div className="min-h-0 flex-1">
                <MessageFlowVisualizer
                    result={parseResult}
                    onExpandItem={handleExpandItem}
                    summaryHint={sameProtocol ? t('rawLockedHint') : t('sourceSwitchHint')}
                    summaryActions={<VisualSourceSwitch disabled={sameProtocol} />}
                />
            </div>
            {expandedItem && (
                <MessageFlowExpandedContentOverlay
                    item={expandedItem}
                    contentMode={visualContentMode}
                    onContentModeChange={setVisualContentMode}
                    onClose={() => setExpandedContent(null)}
                    contentRef={expandedOverlayRef}
                />
            )}
        </div>
    );
}

function LogContentPanels({ log, scope, onOpenLog }: { log: RelayLog; scope: LogScope; onOpenLog?: (id: number) => void }) {
    const { isOpen } = useMorphingDialog();
    const openStateRef = useRef(false);
    const shouldFetchDetail = isOpen && !!log.content_omitted;
    const detailQuery = useLogDetail({ id: log.id, scope, enabled: shouldFetchDetail });

    const activeDetailTab = useLogDetailStore((state) => state.activeDetailTab);
    const setActiveVisualItemId = useLogDetailStore((state) => state.setActiveVisualItemId);
    const data: LogDetailContentData = {
        originalRequestContent: detailQuery.data?.original_request_content ?? log.original_request_content,
        outboundRequestContent: detailQuery.data?.outbound_request_content ?? log.outbound_request_content,
        originalResponseContent: detailQuery.data?.original_response_content ?? log.original_response_content,
        streamPreviewContent: detailQuery.data?.stream_preview_content ?? log.stream_preview_content,
        responseContent: detailQuery.data?.response_content ?? log.response_content,
        originalRequestProtocol: detailQuery.data?.original_request_protocol ?? log.original_request_protocol,
        outboundRequestProtocol: detailQuery.data?.outbound_request_protocol ?? log.outbound_request_protocol,
        originalRequestParsedContent: detailQuery.data?.parsed_original_request_content ?? log.parsed_original_request_content,
        outboundRequestParsedContent: detailQuery.data?.parsed_outbound_request_content ?? log.parsed_outbound_request_content,
        originalResponseParsedContent: detailQuery.data?.parsed_original_response_content ?? log.parsed_original_response_content,
        streamPreviewParsedContent: detailQuery.data?.parsed_stream_preview_content ?? log.parsed_stream_preview_content,
        responseParsedContent: detailQuery.data?.parsed_response_content ?? log.parsed_response_content,
        isDetailLoading: shouldFetchDetail && detailQuery.isLoading && !detailQuery.data,
        isDetailLoadFailed: shouldFetchDetail && !detailQuery.data && !!detailQuery.error,
    };

    useEffect(() => {
        if (isOpen && !openStateRef.current) {
            setActiveVisualItemId(null);
            onOpenLog?.(log.id);
        }
        openStateRef.current = isOpen;
    }, [isOpen, log.id, onOpenLog, setActiveVisualItemId]);

    return (
        <div className="relative flex-1 min-h-0 overflow-hidden">
            <div
                className={cn('absolute inset-0 min-h-0', activeDetailTab !== 'json_sse' && 'pointer-events-none invisible')}
                aria-hidden={activeDetailTab !== 'json_sse'}
            >
                <JsonSseLogDetailView log={log} data={data} />
            </div>
            <div
                className={cn('absolute inset-0 min-h-0', activeDetailTab !== 'visualization' && 'pointer-events-none invisible')}
                aria-hidden={activeDetailTab !== 'visualization'}
            >
                <VisualMessageFlowPanel data={data} />
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
                        <DetailTabSwitch />
                        <MorphingDialogTitle className="flex max-w-[calc(100%-9rem)] items-center gap-2 mb-3 pr-3 text-sm">
                            <ModelAvatar size={28} />
                            <span className="truncate font-semibold text-card-foreground">{log.request_model_name}</span>
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
                                                                            attemptTone(attempt.status) === 'success'
                                                                                ? "bg-primary/5 border-primary/20 hover:bg-primary/10"
                                                                                : attemptTone(attempt.status) === 'neutral'
                                                                                    ? "bg-secondary/40 border-border/50 hover:bg-secondary/60"
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
