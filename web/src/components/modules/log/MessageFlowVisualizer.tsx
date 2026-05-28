'use client';

// [fork] Render parsed AI request/response payloads as a single message flow.
import { useEffect, useMemo } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Bot, Braces, Code2, FileText, Hammer, MessageSquare, Sparkles, User, Wrench } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion';
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
        <div className="inline-flex rounded-lg border border-border bg-background p-0.5">
            {(['source', 'preview'] as const).map((value) => (
                <Button
                    key={value}
                    type="button"
                    variant={mode === value ? 'secondary' : 'ghost'}
                    size="sm"
                    className={cn(
                        'h-7 rounded-md px-2 text-xs',
                        mode !== value && 'text-muted-foreground hover:text-foreground'
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
        <div className="prose prose-sm max-w-none text-sm leading-relaxed text-foreground dark:prose-invert prose-headings:mt-3 prose-headings:mb-2 prose-p:my-2 prose-pre:my-2 prose-pre:rounded-lg prose-pre:border prose-pre:border-border prose-pre:bg-muted prose-code:text-xs prose-table:text-xs prose-a:text-primary">
            <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={{
                    a: ({ href, children }) => (
                        <a href={href} target="_blank" rel="noreferrer">
                            {children}
                        </a>
                    ),
                    pre: ({ children }) => (
                        <pre className="overflow-auto whitespace-pre-wrap break-words p-3">
                            {children}
                        </pre>
                    ),
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
        <pre className="overflow-auto whitespace-pre-wrap break-words rounded-lg border border-border bg-background p-3 text-xs leading-relaxed text-foreground">
            {content}
        </pre>
    );
}

function ToolCallList({ toolCalls }: { toolCalls: MessageFlowToolCall[] }) {
    const t = useTranslations('log.card.visual');

    return (
        <div className="flex flex-col gap-2">
            {toolCalls.map((call, index) => (
                <div key={`${call.id ?? call.name}-${index}`} className="rounded-lg border border-border bg-background p-3">
                    <div className="mb-2 flex items-center gap-2">
                        <Badge variant="secondary" className="text-xs">
                            {call.name || `${t('toolCall')} ${index + 1}`}
                        </Badge>
                        {call.id && <span className="font-mono text-[11px] text-muted-foreground">{call.id}</span>}
                    </div>
                    {call.arguments && (
                        <pre className="overflow-auto whitespace-pre-wrap break-words text-xs leading-relaxed text-muted-foreground">
                            {call.arguments}
                        </pre>
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
                        <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-2 text-[11px] leading-relaxed text-muted-foreground">
                            {formatMessageFlowJson(tool.parameters)}
                        </pre>
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
        <AccordionItem value={item.id} className="rounded-xl border border-border bg-card px-3 shadow-sm">
            <AccordionTrigger className="gap-3 py-3 hover:no-underline">
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
                        <p className="truncate text-sm font-medium text-foreground">{item.title}</p>
                        <p className="mt-1 line-clamp-2 text-xs leading-relaxed text-muted-foreground">{summary}</p>
                    </div>
                </div>
            </AccordionTrigger>
            <AccordionContent className="pb-3">
                <div className="flex flex-col gap-4 border-t border-border pt-3">
                    {(hasContent || hasReasoning) && (
                        <div className="flex items-center justify-between gap-3">
                            <SectionTitle icon={<Braces className="size-3.5" />}>{t('content')}</SectionTitle>
                            <ContentModeSwitch mode={contentMode} onChange={onContentModeChange} />
                        </div>
                    )}

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
            </AccordionContent>
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

    const firstItemId = useMemo(() => firstRenderableItemId(result.items), [result.items]);
    const activeItemExists = result.items.some((item) => item.id === activeVisualItemId);
    const accordionValue = activeItemExists ? activeVisualItemId : firstItemId;

    useEffect(() => {
        if (!firstItemId) {
            if (activeVisualItemId !== null) setActiveVisualItemId(null);
            return;
        }
        if (!activeVisualItemId || !activeItemExists) {
            setActiveVisualItemId(firstItemId);
        }
    }, [activeItemExists, activeVisualItemId, firstItemId, setActiveVisualItemId]);

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
                    collapsible={false}
                    value={accordionValue ?? undefined}
                    onValueChange={(value) => {
                        if (value) setActiveVisualItemId(value);
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
