'use client';

// [fork] Render parsed AI request/response payloads as a single message flow.
import { useEffect, useMemo, useRef } from 'react';
import * as AccordionPrimitive from '@radix-ui/react-accordion';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Bot, Braces, Code2, FileText, Hammer, MessageSquare, Sparkles, User, Wrench } from 'lucide-react';
import { useTranslations } from 'next-intl';
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

    return (
        <div className="inline-flex rounded-lg border border-border bg-background p-0.5 shadow-xs">
            {(['source', 'preview'] as const).map((value) => (
                <Button
                    key={value}
                    type="button"
                    variant={mode === value ? 'default' : 'ghost'}
                    size="sm"
                    aria-pressed={mode === value}
                    className={cn(
                        'h-7 rounded-md px-2 text-xs',
                        mode === value
                            ? 'bg-primary text-primary-foreground shadow-sm hover:bg-primary/90'
                            : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                    )}
                    onClick={() => onChange(value)}
                >
                    {value === 'source' ? <Code2 className="size-3.5" /> : <FileText className="size-3.5" />}
                    {value === 'source' ? t('source') : t('preview')}
                </Button>
            ))}
        </div>
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
                        <pre className="my-3 overflow-auto whitespace-pre-wrap break-words rounded-lg border border-border bg-muted p-3 font-mono text-xs leading-relaxed">
                            {children}
                        </pre>
                    ),
                    table: ({ children }) => (
                        <div className="my-3 overflow-auto rounded-lg border border-border">
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

function JsonDetails({ children }: { children: React.ReactNode }) {
    const t = useTranslations('log.card.visual');

    return (
        <details className="group mt-2 rounded-md border border-border bg-muted/40">
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

function ToolCallList({ toolCalls }: { toolCalls: MessageFlowToolCall[] }) {
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
                        <JsonDetails>
                            <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-words text-xs leading-relaxed text-muted-foreground">
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
                            <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-words text-[11px] leading-relaxed text-muted-foreground">
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

function MessageFlowAccordionItem({
    item,
    contentMode,
    onContentModeChange,
}: {
    item: MessageFlowItem;
    contentMode: LogVisualContentMode;
    onContentModeChange: (mode: LogVisualContentMode) => void;
}) {
    const t = useTranslations('log.card.visual');
    const summary = compactText(item.content ?? item.reasoning, t('noContent'));
    const hasContent = !!item.content?.trim();
    const hasReasoning = !!item.reasoning?.trim();
    const hasToolCalls = (item.toolCalls?.length ?? 0) > 0;
    const hasTools = (item.tools?.length ?? 0) > 0;

    return (
        <AccordionItem value={item.id} className="min-w-0 overflow-hidden rounded-xl border border-border bg-card px-3 shadow-sm data-[state=open]:max-h-[min(72vh,calc(100vh-14rem))] data-[state=open]:min-h-[18rem] data-[state=open]:flex data-[state=open]:flex-col">
            <AccordionTrigger className="sticky top-0 z-10 gap-3 bg-card py-3 hover:no-underline">
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
            <AccordionPrimitive.Content className="data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down min-h-0 flex-1 overflow-hidden text-sm">
                <div className="flex h-full min-h-0 flex-col border-t border-border pb-3">
                    {(hasContent || hasReasoning) && (
                        <div className="flex shrink-0 justify-end bg-card/95 py-3 backdrop-blur">
                            <ContentModeSwitch mode={contentMode} onChange={onContentModeChange} />
                        </div>
                    )}

                    <div className="min-h-0 flex-1 overflow-auto rounded-lg border border-border bg-background/70 p-3">
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
                                    <ToolCallList toolCalls={item.toolCalls ?? []} />
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
            </AccordionPrimitive.Content>
        </AccordionItem>
    );
}

function firstRenderableItemId(items: MessageFlowItem[]): string | null {
    return items.find((item) => (
        item.content?.trim() || item.reasoning?.trim() || (item.toolCalls?.length ?? 0) > 0 || (item.tools?.length ?? 0) > 0
    ))?.id ?? null;
}

export function MessageFlowVisualizer({ result }: { result: MessageFlowParseResult }) {
    const t = useTranslations('log.card.visual');
    const activeVisualItemId = useLogDetailStore((state) => state.activeVisualItemId);
    const setActiveVisualItemId = useLogDetailStore((state) => state.setActiveVisualItemId);
    const visualContentMode = useLogDetailStore((state) => state.visualContentMode);
    const setVisualContentMode = useLogDetailStore((state) => state.setVisualContentMode);
    const initializedResultKeyRef = useRef<string | null>(null);

    const firstItemId = useMemo(() => firstRenderableItemId(result.items), [result.items]);
    const activeItemExists = result.items.some((item) => item.id === activeVisualItemId);
    const accordionValue = activeItemExists ? activeVisualItemId : null;
    const resultKey = useMemo(() => (
        `${result.sourceMode}:${result.protocolPair.request}:${result.protocolPair.response}:${result.items.map((item) => item.id).join('|')}`
    ), [result.items, result.protocolPair.request, result.protocolPair.response, result.sourceMode]);

    useEffect(() => {
        if (!firstItemId) {
            if (activeVisualItemId !== null) setActiveVisualItemId(null);
            initializedResultKeyRef.current = resultKey;
            return;
        }
        if (initializedResultKeyRef.current !== resultKey) {
            initializedResultKeyRef.current = resultKey;
            setActiveVisualItemId(firstItemId);
            return;
        }
        if (activeVisualItemId && !activeItemExists) {
            setActiveVisualItemId(firstItemId);
        }
    }, [activeItemExists, activeVisualItemId, firstItemId, resultKey, setActiveVisualItemId]);

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
        <div className="flex h-full min-h-0 flex-col gap-3">
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
            <div className="min-h-0 flex-1 overflow-auto pr-1">
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
                        />
                    ))}
                </Accordion>
            </div>
        </div>
    );
}
