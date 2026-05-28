'use client';

// [fork] Render parsed AI request/response payloads as a single message flow.
import { type RefObject, type WheelEvent, useEffect, useMemo, useRef } from 'react';
import * as AccordionPrimitive from '@radix-ui/react-accordion';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { ArrowDownToLine, ArrowUpToLine, Bot, Braces, Code2, FileText, Hammer, Maximize2, MessageSquare, Sparkles, User, X, Wrench } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { CopyIconButton } from '@/components/common/CopyButton';
import { Accordion, AccordionItem, AccordionTrigger } from '@/components/ui/accordion';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import {
    formatMessageFlowJson,
    type MessageFlowItem,
    type MessageFlowParseResult,
    type MessageFlowRole,
    type MessageFlowTool,
    type MessageFlowToolCall,
} from './message-flow-parser';
import { useLogDetailStore, type LogVisualContentMode } from './detail-store';

const visibleScrollbarClass = '[scrollbar-color:rgba(120,120,120,0.48)_transparent] [scrollbar-width:thin] [-ms-overflow-style:auto] [&::-webkit-scrollbar]:block [&::-webkit-scrollbar]:size-2 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-muted-foreground/35 [&::-webkit-scrollbar-thumb:hover]:bg-muted-foreground/55 [&::-webkit-scrollbar-track]:bg-transparent';
type ScrollEdge = 'top' | 'bottom';

function handleScrollableWheelBoundary(event: WheelEvent<HTMLElement>) {
    const element = event.currentTarget;
    const maxScrollTop = element.scrollHeight - element.clientHeight;
    if (maxScrollTop <= 1 || event.deltaY === 0) return;

    const canScrollUp = element.scrollTop > 0;
    const canScrollDown = element.scrollTop < maxScrollTop - 1;
    if ((event.deltaY < 0 && canScrollUp) || (event.deltaY > 0 && canScrollDown)) {
        event.stopPropagation();
    }
}

function scrollElementToEdge(element: HTMLElement | null, edge: ScrollEdge) {
    element?.scrollTo({
        top: edge === 'top' ? 0 : element.scrollHeight,
        behavior: 'smooth',
    });
}

function roleIcon(role: MessageFlowRole) {
    switch (role) {
        case 'assistant':
            return <Bot className="size-4 text-violet-500" />;
        case 'user':
            return <User className="size-4 text-sky-500" />;
        case 'system':
            return <Sparkles className="size-4 text-amber-500" />;
        case 'tool':
            return <Wrench className="size-4 text-emerald-500" />;
        default:
            return <MessageSquare className="size-4 text-muted-foreground" />;
    }
}

function roleTone(role: MessageFlowRole): string {
    switch (role) {
        case 'assistant':
            return 'bg-violet-500/10 text-violet-600 dark:text-violet-300';
        case 'user':
            return 'bg-sky-500/10 text-sky-600 dark:text-sky-300';
        case 'system':
            return 'bg-amber-500/10 text-amber-700 dark:text-amber-300';
        case 'tool':
            return 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-300';
        default:
            return 'bg-secondary text-secondary-foreground';
    }
}

function protocolLabel(protocol: string): string {
    switch (protocol) {
        case 'openai_chat':
            return 'OpenAI Chat';
        case 'openai_responses':
            return 'OpenAI Responses';
        case 'anthropic_messages':
            return 'Anthropic Messages';
        default:
            return protocol || '-';
    }
}

function compactText(value: string | undefined, fallback: string): string {
    const text = value?.replace(/\s+/g, ' ').trim();
    if (!text) return fallback;
    return text.length > 140 ? `${text.slice(0, 140)}...` : text;
}

function itemSummary(item: MessageFlowItem, fallback: string, toolsLabel: string): string {
    const tools = item.tools ?? [];
    if (tools.length > 0) {
        const names = tools.map((tool) => tool.name).filter(Boolean).join(', ');
        return names ? `${tools.length} ${toolsLabel}: ${compactText(names, fallback)}` : `${tools.length} ${toolsLabel}`;
    }
    return compactText(item.content ?? item.reasoning, fallback);
}

function itemCopyText(item: MessageFlowItem): string {
    const parts: string[] = [];
    if (item.reasoning?.trim()) parts.push(`## Reasoning\n${item.reasoning.trim()}`);
    if (item.content?.trim()) parts.push(`## Content\n${item.content.trim()}`);
    if ((item.toolCalls?.length ?? 0) > 0) parts.push(`## Tool Calls\n${formatMessageFlowJson(item.toolCalls)}`);
    if ((item.tools?.length ?? 0) > 0) parts.push(`## Tools\n${formatMessageFlowJson(item.tools?.map((tool) => tool.raw) ?? [])}`);
    return parts.join('\n\n');
}

function SectionTitle({ icon, children }: { icon: React.ReactNode; children: React.ReactNode }) {
    return (
        <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {icon}
            <span>{children}</span>
        </div>
    );
}

function ContentModeSwitch({
    mode,
    onChange,
}: {
    mode: LogVisualContentMode;
    onChange: (mode: LogVisualContentMode) => void;
}) {
    const t = useTranslations('log.card.visual');
    const modes: Array<{ value: LogVisualContentMode; label: string; icon: React.ReactNode }> = [
        { value: 'source', label: t('source'), icon: <Code2 className="size-3.5" /> },
        { value: 'preview', label: t('preview'), icon: <FileText className="size-3.5" /> },
    ];

    return (
        <div className="inline-flex rounded-lg border border-border bg-background p-0.5 shadow-xs">
            {modes.map((item) => (
                <Button
                    key={item.value}
                    type="button"
                    variant={mode === item.value ? 'default' : 'ghost'}
                    size="icon-sm"
                    aria-label={item.label}
                    title={item.label}
                    aria-pressed={mode === item.value}
                    className={cn(
                        'size-7 rounded-md p-0',
                        mode === item.value
                            ? 'bg-primary text-primary-foreground shadow-sm hover:bg-primary/90'
                            : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                    )}
                    onClick={() => onChange(item.value)}
                >
                    {item.icon}
                </Button>
            ))}
        </div>
    );
}

function FloatingIconButton({
    label,
    children,
    onClick,
}: {
    label: string;
    children: React.ReactNode;
    onClick: () => void;
}) {
    return (
        <button
            type="button"
            aria-label={label}
            title={label}
            className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            onClick={onClick}
        >
            {children}
        </button>
    );
}

function ContentFloatingActions({
    targetRef,
    onExpand,
    children,
}: {
    targetRef: RefObject<HTMLElement | null>;
    onExpand?: () => void;
    children?: React.ReactNode;
}) {
    const t = useTranslations('log.card.visual');

    return (
        <>
            <div className="pointer-events-auto absolute right-3.5 top-2 z-20 flex items-center gap-1.5">
                {children}
                <div className="inline-flex rounded-lg border border-border bg-background p-0.5 shadow-xs">
                    {onExpand && (
                        <FloatingIconButton label={t('expandContent')} onClick={onExpand}>
                            <Maximize2 className="size-3.5" />
                        </FloatingIconButton>
                    )}
                    <FloatingIconButton label={t('scrollTop')} onClick={() => scrollElementToEdge(targetRef.current, 'top')}>
                        <ArrowUpToLine className="size-3.5" />
                    </FloatingIconButton>
                </div>
            </div>
            <div className="pointer-events-auto absolute bottom-2 right-3.5 z-20 inline-flex rounded-lg border border-border bg-background p-0.5 shadow-xs">
                <FloatingIconButton label={t('scrollBottom')} onClick={() => scrollElementToEdge(targetRef.current, 'bottom')}>
                    <ArrowDownToLine className="size-3.5" />
                </FloatingIconButton>
            </div>
        </>
    );
}

function MarkdownContent({ content }: { content: string }) {
    return (
        <div className="text-sm leading-relaxed text-foreground">
            <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={{
                    h1: ({ children }) => <h1 className="mb-3 mt-1 border-b border-border pb-2 text-2xl font-bold leading-tight">{children}</h1>,
                    h2: ({ children }) => <h2 className="mb-2 mt-5 border-b border-border/70 pb-1.5 text-xl font-semibold leading-tight">{children}</h2>,
                    h3: ({ children }) => <h3 className="mb-2 mt-4 text-lg font-semibold leading-tight">{children}</h3>,
                    h4: ({ children }) => <h4 className="mb-2 mt-3 text-base font-semibold leading-tight">{children}</h4>,
                    h5: ({ children }) => <h5 className="mb-2 mt-3 text-sm font-semibold uppercase tracking-wide text-muted-foreground">{children}</h5>,
                    h6: ({ children }) => <h6 className="mb-2 mt-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{children}</h6>,
                    p: ({ children }) => <p className="my-2">{children}</p>,
                    ul: ({ children }) => <ul className="my-2 list-disc space-y-1 pl-5">{children}</ul>,
                    ol: ({ children }) => <ol className="my-2 list-decimal space-y-1 pl-5">{children}</ol>,
                    li: ({ children }) => <li className="pl-1">{children}</li>,
                    blockquote: ({ children }) => (
                        <blockquote className="my-3 border-l-4 border-primary/40 bg-muted/50 px-3 py-2 text-muted-foreground">
                            {children}
                        </blockquote>
                    ),
                    hr: () => <hr className="my-4 border-border" />,
                    strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
                    em: ({ children }) => <em className="italic">{children}</em>,
                    a: ({ href, children }) => (
                        <a href={href} target="_blank" rel="noreferrer" className="text-primary underline underline-offset-2">
                            {children}
                        </a>
                    ),
                    code: ({ className, children }) => (
                        <code className={cn('font-mono text-[0.85em] text-foreground', !className && 'rounded bg-muted px-1.5 py-0.5')}>
                            {children}
                        </code>
                    ),
                    pre: ({ children }) => (
                        <pre
                            className={cn('my-3 overflow-auto overscroll-auto whitespace-pre-wrap break-words rounded-lg border border-border bg-muted p-3 font-mono text-xs leading-relaxed', visibleScrollbarClass)}
                            onWheel={handleScrollableWheelBoundary}
                        >
                            {children}
                        </pre>
                    ),
                    table: ({ children }) => (
                        <div
                            className={cn('my-3 overflow-auto overscroll-auto rounded-lg border border-border', visibleScrollbarClass)}
                            onWheel={handleScrollableWheelBoundary}
                        >
                            <table className="w-full border-collapse text-left text-xs">{children}</table>
                        </div>
                    ),
                    thead: ({ children }) => <thead className="bg-muted text-foreground">{children}</thead>,
                    th: ({ children }) => <th className="border-b border-border px-3 py-2 font-semibold">{children}</th>,
                    td: ({ children }) => <td className="border-t border-border px-3 py-2 align-top">{children}</td>,
                }}
            >
                {content}
            </ReactMarkdown>
        </div>
    );
}

function TextBlock({
    content,
    mode,
}: {
    content: string;
    mode: LogVisualContentMode;
}) {
    if (mode === 'preview') {
        return <MarkdownContent content={content} />;
    }

    return (
        <pre className="whitespace-pre-wrap break-words text-xs leading-relaxed text-foreground">
            {content}
        </pre>
    );
}

function JsonDetails({ children, defaultOpen = false }: { children: React.ReactNode; defaultOpen?: boolean }) {
    const t = useTranslations('log.card.visual');

    return (
        <details open={defaultOpen} className="group mt-2 rounded-md border border-border bg-muted/40">
            <summary className="flex cursor-pointer list-none items-center gap-2 rounded-md px-2 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground">
                <Code2 className="size-3.5" />
                <span>{t('jsonDetails')}</span>
                <span className="ml-auto text-[10px] uppercase tracking-wide group-open:hidden">{t('collapsed')}</span>
                <span className="ml-auto hidden text-[10px] uppercase tracking-wide group-open:inline">{t('expanded')}</span>
            </summary>
            <div className="border-t border-border p-2">
                {children}
            </div>
        </details>
    );
}

function ToolCallList({ toolCalls, defaultOpenJson = false }: { toolCalls: MessageFlowToolCall[]; defaultOpenJson?: boolean }) {
    const t = useTranslations('log.card.visual');

    return (
        <div className="flex flex-col gap-2">
            {toolCalls.map((call, index) => (
                <div key={`${call.id ?? call.name}-${index}`} className="rounded-lg border border-border bg-background p-3">
                    <div className="flex min-w-0 items-center gap-2">
                        <Badge variant="secondary" className="text-xs">
                            {call.name || `${t('toolCall')} ${index + 1}`}
                        </Badge>
                        {call.id && <span className="font-mono text-[11px] text-muted-foreground">{call.id}</span>}
                    </div>
                    {call.arguments && (
                        <JsonDetails defaultOpen={defaultOpenJson}>
                            <pre
                                className={cn('max-h-60 overflow-auto overscroll-auto whitespace-pre-wrap break-words text-xs leading-relaxed text-muted-foreground', visibleScrollbarClass)}
                                onWheel={handleScrollableWheelBoundary}
                            >
                                {call.arguments}
                            </pre>
                        </JsonDetails>
                    )}
                </div>
            ))}
        </div>
    );
}

function ToolConfigList({ tools }: { tools: MessageFlowTool[] }) {
    const t = useTranslations('log.card.visual');

    return (
        <div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
            {tools.map((tool, index) => (
                <div key={`${tool.id}-${index}`} className="min-w-0 rounded-lg border border-border bg-background p-3">
                    <div className="mb-2 flex min-w-0 items-center gap-2">
                        <Badge variant="secondary" className="shrink-0 text-xs">
                            {tool.type}
                        </Badge>
                        <span className="truncate text-sm font-medium text-foreground">{tool.name}</span>
                    </div>
                    {tool.description && (
                        <p className="mb-2 text-xs leading-relaxed text-muted-foreground">{tool.description}</p>
                    )}
                    {typeof tool.parameters !== 'undefined' && (
                        <JsonDetails>
                            <pre
                                className={cn('max-h-60 overflow-auto overscroll-auto whitespace-pre-wrap break-words text-[11px] leading-relaxed text-muted-foreground', visibleScrollbarClass)}
                                onWheel={handleScrollableWheelBoundary}
                            >
                                {formatMessageFlowJson(tool.parameters)}
                            </pre>
                        </JsonDetails>
                    )}
                    {typeof tool.parameters === 'undefined' && (
                        <p className="text-xs text-muted-foreground">{t('noToolParameters')}</p>
                    )}
                </div>
            ))}
        </div>
    );
}

function MessageFlowContentPanel({
    item,
    contentMode,
    onContentModeChange,
    onExpand,
    expanded = false,
}: {
    item: MessageFlowItem;
    contentMode: LogVisualContentMode;
    onContentModeChange: (mode: LogVisualContentMode) => void;
    onExpand?: () => void;
    expanded?: boolean;
}) {
    const t = useTranslations('log.card.visual');
    const scrollRef = useRef<HTMLDivElement | null>(null);
    const hasContent = !!item.content?.trim();
    const hasReasoning = !!item.reasoning?.trim();
    const hasToolCalls = (item.toolCalls?.length ?? 0) > 0;
    const hasTools = (item.tools?.length ?? 0) > 0;
    const copyText = itemCopyText(item);
    const hasCopyableContent = !!copyText.trim();
    const hasModeSwitch = hasContent || hasReasoning;
    const hasFloatingActions = hasCopyableContent || hasModeSwitch || !!onExpand;

    return (
        <div
            className={cn(
                'relative min-h-0 overflow-hidden rounded-lg border border-border bg-background/70',
                expanded ? 'h-full flex-1' : 'max-h-[300px]'
            )}
        >
            <ContentFloatingActions targetRef={scrollRef} onExpand={onExpand}>
                {hasCopyableContent && (
                    <div className="inline-flex rounded-lg border border-border bg-background p-0.5 shadow-xs">
                        <CopyIconButton
                            text={copyText}
                            title={t('copy')}
                            className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                            copyIconClassName="size-3.5"
                            checkIconClassName="size-3.5"
                        />
                    </div>
                )}
                {hasModeSwitch && (
                    <ContentModeSwitch mode={contentMode} onChange={onContentModeChange} />
                )}
            </ContentFloatingActions>
            <div
                ref={scrollRef}
                className={cn(
                    'min-h-0 overflow-y-auto overscroll-auto p-3 pb-10',
                    expanded ? 'h-full' : 'max-h-[300px]',
                    visibleScrollbarClass,
                    hasFloatingActions && (expanded ? 'pr-36' : 'pr-32')
                )}
                onWheel={handleScrollableWheelBoundary}
            >
                <div className="flex flex-col gap-4">
                    {hasReasoning && (
                        <div className="flex flex-col gap-2">
                            <SectionTitle icon={<Sparkles className="size-3.5" />}>{t('reasoning')}</SectionTitle>
                            <TextBlock content={item.reasoning ?? ''} mode={contentMode} />
                        </div>
                    )}

                    {hasContent && (
                        <div className="flex flex-col gap-2">
                            <SectionTitle icon={<MessageSquare className="size-3.5" />}>{t('content')}</SectionTitle>
                            <TextBlock content={item.content ?? ''} mode={contentMode} />
                        </div>
                    )}

                    {hasToolCalls && (
                        <div className="flex flex-col gap-2">
                            <SectionTitle icon={<Hammer className="size-3.5" />}>{t('toolCalls')}</SectionTitle>
                            <ToolCallList toolCalls={item.toolCalls ?? []} defaultOpenJson={item.source === 'response'} />
                        </div>
                    )}

                    {hasTools && (
                        <div className="flex flex-col gap-2">
                            <SectionTitle icon={<Wrench className="size-3.5" />}>{t('tools')}</SectionTitle>
                            <ToolConfigList tools={item.tools ?? []} />
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

export function MessageFlowExpandedContentOverlay({
    item,
    contentMode,
    onContentModeChange,
    onClose,
}: {
    item: MessageFlowItem;
    contentMode: LogVisualContentMode;
    onContentModeChange: (mode: LogVisualContentMode) => void;
    onClose: () => void;
}) {
    const t = useTranslations('log.card.visual');

    return (
        <div className="absolute inset-0 z-40 flex min-h-0 flex-col rounded-2xl border border-border bg-card p-3 shadow-2xl md:p-4">
            <div className="mb-3 flex shrink-0 items-center gap-3">
                <div className="shrink-0">{roleIcon(item.role)}</div>
                <div className="min-w-0 flex-1">
                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <Badge className={cn('border-0 text-xs shadow-none', roleTone(item.role))}>
                            {item.originalRole || item.role}
                        </Badge>
                        <Badge variant="outline" className="text-[11px]">
                            {item.source === 'request' ? t('request') : t('response')}
                        </Badge>
                        <span className="truncate text-xs text-muted-foreground">{protocolLabel(item.protocol)}</span>
                    </div>
                    <p className="mt-1 truncate text-xs text-muted-foreground">{itemSummary(item, t('noContent'), t('tools'))}</p>
                </div>
                <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t('closeExpandedContent')}
                    title={t('closeExpandedContent')}
                    className="size-8 rounded-lg text-muted-foreground hover:text-foreground"
                    onClick={onClose}
                >
                    <X className="size-4" />
                </Button>
            </div>
            <MessageFlowContentPanel
                item={item}
                contentMode={contentMode}
                onContentModeChange={onContentModeChange}
                expanded
            />
        </div>
    );
}

function MessageFlowAccordionItem({
    item,
    contentMode,
    onContentModeChange,
    onExpand,
}: {
    item: MessageFlowItem;
    contentMode: LogVisualContentMode;
    onContentModeChange: (mode: LogVisualContentMode) => void;
    onExpand: (item: MessageFlowItem) => void;
}) {
    const t = useTranslations('log.card.visual');
    const summary = itemSummary(item, t('noContent'), t('tools'));
    const hasToolCalls = (item.toolCalls?.length ?? 0) > 0;
    const hasTools = (item.tools?.length ?? 0) > 0;

    return (
        <AccordionItem value={item.id} className="min-w-0 overflow-hidden rounded-xl border border-border bg-card px-3 shadow-sm">
            <AccordionTrigger className="shrink-0 gap-3 bg-card py-3 hover:no-underline">
                <div className="flex min-w-0 flex-1 items-start gap-3 text-left">
                    <div className="mt-0.5 shrink-0">{roleIcon(item.role)}</div>
                    <div className="min-w-0 flex-1">
                        <div className="mb-1 flex min-w-0 flex-wrap items-center gap-2">
                            <Badge className={cn('border-0 text-xs shadow-none', roleTone(item.role))}>
                                {item.originalRole || item.role}
                            </Badge>
                            <Badge variant="outline" className="text-[11px]">
                                {item.source === 'request' ? t('request') : t('response')}
                            </Badge>
                            <span className="truncate text-xs text-muted-foreground">{protocolLabel(item.protocol)}</span>
                            {hasToolCalls && (
                                <Badge variant="secondary" className="text-[11px]">
                                    <Hammer className="size-3" />
                                    {item.toolCalls?.length} {t('toolCalls')}
                                </Badge>
                            )}
                            {hasTools && (
                                <Badge variant="secondary" className="text-[11px]">
                                    <Wrench className="size-3" />
                                    {item.tools?.length} {t('tools')}
                                </Badge>
                            )}
                        </div>
                        <p className="mt-1 line-clamp-2 text-xs leading-relaxed text-muted-foreground">{summary}</p>
                    </div>
                </div>
            </AccordionTrigger>
            <AccordionPrimitive.Content className="flex min-h-0 flex-1 flex-col overflow-hidden text-sm">
                <div className="border-t border-border py-3">
                    <MessageFlowContentPanel
                        item={item}
                        contentMode={contentMode}
                        onContentModeChange={onContentModeChange}
                        onExpand={() => onExpand(item)}
                    />
                </div>
            </AccordionPrimitive.Content>
        </AccordionItem>
    );
}

export function MessageFlowVisualizer({
    result,
    onExpandItem,
}: {
    result: MessageFlowParseResult;
    onExpandItem?: (item: MessageFlowItem) => void;
}) {
    const t = useTranslations('log.card.visual');
    const activeVisualItemId = useLogDetailStore((state) => state.activeVisualItemId);
    const setActiveVisualItemId = useLogDetailStore((state) => state.setActiveVisualItemId);
    const visualContentMode = useLogDetailStore((state) => state.visualContentMode);
    const setVisualContentMode = useLogDetailStore((state) => state.setVisualContentMode);
    const previousResultKeyRef = useRef<string | null>(null);

    const activeItemExists = result.items.some((item) => item.id === activeVisualItemId);
    const accordionValue = activeItemExists ? activeVisualItemId : null;
    const resultKey = useMemo(() => (
        `${result.sourceMode}:${result.protocolPair.request}:${result.protocolPair.response}:${result.items.map((item) => item.id).join('|')}`
    ), [result.items, result.protocolPair.request, result.protocolPair.response, result.sourceMode]);

    useEffect(() => {
        if (previousResultKeyRef.current !== resultKey) {
            previousResultKeyRef.current = resultKey;
            if (activeVisualItemId !== null) {
                setActiveVisualItemId(null);
            }
            return;
        }
        if (activeVisualItemId && !activeItemExists) {
            setActiveVisualItemId(null);
        }
    }, [activeItemExists, activeVisualItemId, resultKey, setActiveVisualItemId]);

    if (result.error && result.items.length === 0) {
        return (
            <div className="flex h-full items-center justify-center rounded-2xl border border-border bg-muted/30 p-6 text-center">
                <div className="max-w-md">
                    <Braces className="mx-auto mb-3 size-8 text-muted-foreground" />
                    <h3 className="text-sm font-semibold text-foreground">{t('parseFailed')}</h3>
                    <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
                        {t(result.error)}
                    </p>
                </div>
            </div>
        );
    }

    if (result.items.length === 0) {
        return (
            <div className="flex h-full items-center justify-center rounded-2xl border border-border bg-muted/30 p-6 text-center">
                <div className="max-w-md">
                    <MessageSquare className="mx-auto mb-3 size-8 text-muted-foreground" />
                    <h3 className="text-sm font-semibold text-foreground">{t('empty')}</h3>
                    <p className="mt-2 text-xs leading-relaxed text-muted-foreground">{t('emptyHint')}</p>
                </div>
            </div>
        );
    }

    return (
        <div className="relative flex h-full min-h-0 flex-col gap-3 overflow-hidden">
            <div className="flex flex-wrap items-center gap-2 rounded-xl border border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                <Badge variant="secondary" className="text-xs">
                    {result.items.length} {t('items')}
                </Badge>
                <span>{protocolLabel(result.protocolPair.request)}</span>
                <span>→</span>
                <span>{protocolLabel(result.protocolPair.response)}</span>
                {result.warnings.map((warning) => (
                    <Badge key={warning} variant="outline" className="text-[11px]">
                        {t(warning)}
                    </Badge>
                ))}
            </div>
            <div className={cn('min-h-0 flex-1 overflow-auto overscroll-auto pr-1', visibleScrollbarClass)}>
                <Accordion
                    type="single"
                    collapsible
                    value={accordionValue ?? undefined}
                    onValueChange={(value) => {
                        setActiveVisualItemId(value || null);
                    }}
                    className="flex flex-col gap-3"
                >
                    {result.items.map((item) => (
                        <MessageFlowAccordionItem
                            key={item.id}
                            item={item}
                            contentMode={visualContentMode}
                            onContentModeChange={setVisualContentMode}
                            onExpand={onExpandItem ?? (() => undefined)}
                        />
                    ))}
                </Accordion>
            </div>
        </div>
    );
}
