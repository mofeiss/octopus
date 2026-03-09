'use client';

import { useMemo, useState } from 'react';
import { Activity, AlertCircle, CheckCircle2, ChevronDown, Clock, KeyRound, Loader2, PlayCircle, RefreshCw, Server, XCircle } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    GroupChannelCheckItemStatus,
    GroupChannelCheckTaskStatus,
    useSyncGroupChannelCheckTaskStatus,
    useGroupChannelCheckTaskDetail,
} from '@/api/endpoints/group-channel-check';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';

function withTranslationFallback(translated: string, fallback: string, keys: string[]) {
    const normalized = translated.trim();
    if (!normalized) return fallback;
    if (normalized.startsWith('MISSING_MESSAGE') || normalized.startsWith('MISSING_TRANSLATION')) return fallback;
    if (keys.includes(normalized)) return fallback;
    if (keys.some((key) => normalized.endsWith(key))) return fallback;
    return translated;
}

function formatTaskTime(timestamp?: number) {
    if (!timestamp) return '-';
    return new Date(timestamp * 1000).toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false,
    });
}

function formatDuration(durationMs?: number) {
    if (!durationMs) return '-';
    if (durationMs < 1000) return `${durationMs}ms`;
    return `${(durationMs / 1000).toFixed(2)}s`;
}

function StatusBadge({ status }: { status?: GroupChannelCheckTaskStatus | GroupChannelCheckItemStatus }) {
    const t = useTranslations('group.healthCheck.status');

    const text = (() => {
        switch (status) {
            case GroupChannelCheckTaskStatus.Pending:
            case GroupChannelCheckItemStatus.Pending:
                return withTranslationFallback(t('pending'), '待执行', ['group.healthCheck.status.pending', 'pending']);
            case GroupChannelCheckTaskStatus.Running:
            case GroupChannelCheckItemStatus.Running:
                return withTranslationFallback(t('running'), '进行中', ['group.healthCheck.status.running', 'running']);
            case GroupChannelCheckTaskStatus.Success:
            case GroupChannelCheckItemStatus.Success:
                return withTranslationFallback(t('success'), '成功', ['group.healthCheck.status.success', 'success']);
            case GroupChannelCheckTaskStatus.PartialSuccess:
                return withTranslationFallback(t('partialSuccess'), '部分成功', ['group.healthCheck.status.partialSuccess', 'partialSuccess']);
            case GroupChannelCheckTaskStatus.Failed:
            case GroupChannelCheckItemStatus.Failed:
                return withTranslationFallback(t('failed'), '失败', ['group.healthCheck.status.failed', 'failed']);
            default:
                return '-';
        }
    })();

    const className = (() => {
        switch (status) {
            case GroupChannelCheckTaskStatus.Success:
            case GroupChannelCheckItemStatus.Success:
                return 'bg-emerald-500/10 text-emerald-600 border-emerald-500/20';
            case GroupChannelCheckTaskStatus.PartialSuccess:
                return 'bg-amber-500/10 text-amber-600 border-amber-500/20';
            case GroupChannelCheckTaskStatus.Failed:
            case GroupChannelCheckItemStatus.Failed:
                return 'bg-destructive/10 text-destructive border-destructive/20';
            case GroupChannelCheckTaskStatus.Running:
            case GroupChannelCheckItemStatus.Running:
                return 'bg-primary/10 text-primary border-primary/20';
            default:
                return 'bg-muted text-muted-foreground border-border';
        }
    })();

    return (
        <Badge variant="outline" className={cn('rounded-full border px-2.5 py-1 text-xs font-medium', className)}>
            {text}
        </Badge>
    );
}

function PayloadPanel({
    title,
    content,
    className,
}: {
    title: string;
    content?: string;
    className?: string;
}) {
    return (
        <section className={cn('flex min-h-0 flex-col overflow-hidden rounded-2xl border border-border bg-muted/30', className)}>
            <header className="border-b border-border/70 px-4 py-3 text-sm font-medium text-foreground">
                {title}
            </header>
            <div className="min-h-0 flex-1 overflow-auto">
                <pre className="p-4 text-xs leading-relaxed whitespace-pre-wrap break-all text-muted-foreground">
                    {content?.trim() ? content : '-'}
                </pre>
            </div>
        </section>
    );
}

export function GroupChannelCheckDialog({
    open,
    onOpenChange,
    taskId,
    creating,
}: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    taskId?: number | null;
    creating?: boolean;
}) {
    const t = useTranslations('group.healthCheck');
    const taskQuery = useGroupChannelCheckTaskDetail(taskId ?? undefined, open && !!taskId);
    const syncTaskStatus = useSyncGroupChannelCheckTaskStatus();
    const task = taskQuery.data;
    const items = useMemo(() => task?.items ?? [], [task?.items]);
    const [selectedItemId, setSelectedItemId] = useState<number | null>(null);
    const [summaryExpanded, setSummaryExpanded] = useState(false);
    const [detailExpanded, setDetailExpanded] = useState(false);

    const effectiveSelectedItemId = useMemo(() => {
        if (items.length === 0) return null;
        return items.some((item) => item.id === selectedItemId) ? selectedItemId : (items[0]?.id ?? null);
    }, [items, selectedItemId]);

    const selectedItem = useMemo(() => {
        return items.find((item) => item.id === effectiveSelectedItemId) ?? items[0];
    }, [effectiveSelectedItemId, items]);

    const progressText = task
        ? withTranslationFallback(
            t('latest.success', { success: task.success_count, total: task.total_count }),
            `${task.success_count}/${task.total_count}`,
            ['group.healthCheck.latest.success', 'latest.success']
        )
        : '';
    const titleText = withTranslationFallback(t('title'), '渠道测活', ['group.healthCheck.title', 'title']);
    const batchTaskText = withTranslationFallback(t('batchTask'), '批量测活任务', ['group.healthCheck.batchTask', 'batchTask']);
    const singleTaskText = withTranslationFallback(t('singleTask'), '单个渠道测活任务', ['group.healthCheck.singleTask', 'singleTask']);
    const creatingText = withTranslationFallback(t('creating'), '正在创建测活任务...', ['group.healthCheck.creating', 'creating']);
    const loadingText = withTranslationFallback(t('loading'), '正在加载测活结果...', ['group.healthCheck.loading', 'loading']);
    const resultListText = withTranslationFallback(t('resultList'), '测活结果', ['group.healthCheck.resultList', 'resultList']);
    const emptyText = withTranslationFallback(t('empty'), '暂无测活明细', ['group.healthCheck.empty', 'empty']);
    const requestText = withTranslationFallback(t('detail.request'), '请求内容', ['group.healthCheck.detail.request', 'detail.request']);
    const responseText = withTranslationFallback(t('detail.response'), '响应内容', ['group.healthCheck.detail.response', 'detail.response']);
    const metricsRunningText = withTranslationFallback(t('metrics.running'), '进行中', ['group.healthCheck.metrics.running', 'metrics.running']);
    const metricsSuccessText = withTranslationFallback(t('metrics.success'), '成功', ['group.healthCheck.metrics.success', 'metrics.success']);
    const metricsFailedText = withTranslationFallback(t('metrics.failed'), '失败', ['group.healthCheck.metrics.failed', 'metrics.failed']);
    const metricsCreatedAtText = withTranslationFallback(t('metrics.createdAt'), '创建时间', ['group.healthCheck.metrics.createdAt', 'metrics.createdAt']);

    const formatDurationText = (value: string) => withTranslationFallback(
        t('detail.duration', { value }),
        `耗时 ${value}`,
        ['group.healthCheck.detail.duration', 'detail.duration']
    );
    const formatStatusCodeText = (value: string | number) => withTranslationFallback(
        t('detail.statusCode', { value }),
        `状态码 ${value}`,
        ['group.healthCheck.detail.statusCode', 'detail.statusCode']
    );
    const syncStatusText = withTranslationFallback(
        t('actions.syncStatus'),
        '按测活结果同步启用状态',
        ['group.healthCheck.actions.syncStatus', 'actions.syncStatus']
    );

    const handleSyncStatus = async () => {
        if (!taskId) return;
        try {
            const result = await syncTaskStatus.mutateAsync(taskId);
            toast.success(
                withTranslationFallback(
                    t('actions.syncStatusSuccess', {
                        enabled: result.enabled_count,
                        disabled: result.disabled_count,
                        skipped: result.skipped_count,
                    }),
                    `已同步：启用 ${result.enabled_count} 个，禁用 ${result.disabled_count} 个，跳过 ${result.skipped_count} 个`,
                    ['group.healthCheck.actions.syncStatusSuccess', 'actions.syncStatusSuccess']
                )
            );
        } catch (error) {
            const message = error instanceof Error ? error.message : String(error);
            toast.error(
                withTranslationFallback(
                    t('actions.syncStatusFailed'),
                    '同步启用状态失败',
                    ['group.healthCheck.actions.syncStatusFailed', 'actions.syncStatusFailed']
                ),
                { description: message }
            );
        }
    };

    const handleDialogOpenChange = (nextOpen: boolean) => {
        if (!nextOpen) {
            setSummaryExpanded(false);
            setDetailExpanded(false);
            setSelectedItemId(null);
        }

        onOpenChange(nextOpen);
    };

    return (
        <Dialog open={open} onOpenChange={handleDialogOpenChange}>
            <DialogContent
                showCloseButton={false}
                className="h-[calc(100vh-2rem)] w-[calc(100vw-2rem)] max-w-[calc(100vw-2rem)] sm:max-w-[calc(100vw-2rem)] lg:h-[calc(100vh-3rem)] lg:w-[min(1400px,96vw)] lg:max-w-[min(1400px,96vw)] rounded-3xl border-border/70 bg-card px-5 py-4 text-card-foreground custom-shadow flex flex-col overflow-hidden"
            >
                <DialogClose asChild>
                    <Button variant="ghost" size="icon" className="absolute right-4 top-4 rounded-xl text-muted-foreground hover:text-foreground">
                        <XCircle className="size-4" />
                    </Button>
                </DialogClose>

                <div className="mb-0 pr-12">
                    <div className="mb-1.5 flex items-center gap-3">
                        <div className="flex size-10 items-center justify-center rounded-2xl bg-primary/10 text-primary">
                            <Activity className="size-5" />
                        </div>
                        <div className="min-w-0">
                            <DialogTitle className="truncate text-xl font-bold">{task?.group_name || titleText}</DialogTitle>
                            <DialogDescription className="mt-1 text-sm text-muted-foreground">
                                {task?.mode === 'single' ? singleTaskText : batchTaskText}
                            </DialogDescription>
                        </div>
                        <div className="ml-auto shrink-0">
                            <div className="flex items-center gap-2">
                                {task && (
                                    <Badge variant="secondary" className="rounded-full px-2 py-0.5 text-xs">
                                        {progressText}
                                    </Badge>
                                )}
                                <StatusBadge status={task?.status} />
                                {task && (
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="icon"
                                        onClick={() => setSummaryExpanded((value) => !value)}
                                        className="size-8 rounded-xl text-muted-foreground"
                                    >
                                        <ChevronDown className={cn('size-4 transition-transform', !summaryExpanded && '-rotate-90')} />
                                    </Button>
                                )}
                            </div>
                        </div>
                    </div>

                    {task && summaryExpanded && (
                        <div className="grid grid-cols-2 gap-x-4 gap-y-2 pl-[3.25rem] pr-1 text-xs text-muted-foreground md:grid-cols-4">
                            <div className="flex items-center gap-2">
                                <CheckCircle2 className="size-3.5 shrink-0 text-emerald-500" />
                                <span>{metricsSuccessText} {task.success_count}</span>
                            </div>
                            <div className="flex items-center gap-2">
                                <AlertCircle className="size-3.5 shrink-0 text-destructive" />
                                <span>{metricsFailedText} {task.failed_count}</span>
                            </div>
                            <div className="flex items-center gap-2">
                                <Loader2 className="size-3.5 shrink-0 text-primary" />
                                <span>{metricsRunningText} {task.running_count}</span>
                            </div>
                            <div className="flex items-center gap-2">
                                <Clock className="size-3.5 shrink-0 text-muted-foreground" />
                                <span className="truncate">{metricsCreatedAtText} {formatTaskTime(task.created_at)}</span>
                            </div>
                        </div>
                    )}
                </div>

                <div className="min-h-0 flex-1">
                    {creating && !taskId && (
                        <div className="flex h-full flex-col items-center justify-center gap-3 rounded-3xl border border-border/70 bg-muted/20">
                            <Loader2 className="size-8 animate-spin text-primary" />
                            <p className="text-sm text-muted-foreground">{creatingText}</p>
                        </div>
                    )}

                    {!creating && taskQuery.isLoading && taskId && (
                        <div className="flex h-full flex-col items-center justify-center gap-3 rounded-3xl border border-border/70 bg-muted/20">
                            <Loader2 className="size-8 animate-spin text-primary" />
                            <p className="text-sm text-muted-foreground">{loadingText}</p>
                        </div>
                    )}

                    {!creating && taskQuery.error && (
                        <div className="flex h-full flex-col items-center justify-center gap-3 rounded-3xl border border-destructive/30 bg-destructive/5 px-6 text-center">
                            <AlertCircle className="size-8 text-destructive" />
                            <p className="text-sm text-destructive">{taskQuery.error.message}</p>
                        </div>
                    )}

                    {!creating && task && (
                        <div className="grid h-full min-h-0 grid-cols-1 grid-rows-[minmax(0,1.2fr)_minmax(0,0.8fr)] gap-3 xl:grid-cols-[340px_minmax(0,1fr)] xl:grid-rows-1 xl:gap-4">
                            <aside className="flex min-h-0 flex-col overflow-hidden rounded-3xl border border-border/70 bg-muted/20">
                                <div className="border-b border-border/70 px-4 py-3 text-sm font-medium text-foreground">
                                    {resultListText}
                                </div>
                                <div className="min-h-0 flex-1 overflow-auto p-2 space-y-2">
                                    {items.map((item) => (
                                        <button
                                            key={item.id}
                                            type="button"
                                            onClick={() => {
                                                setSelectedItemId(item.id);
                                                setDetailExpanded(false);
                                            }}
                                            className={cn(
                                                'w-full min-w-0 overflow-hidden rounded-2xl border px-3 py-2.5 text-left transition-colors',
                                                selectedItem?.id === item.id
                                                    ? 'border-primary/40 bg-primary/5 shadow-xs'
                                                    : 'border-border/70 bg-background/60 hover:border-border hover:bg-background/80'
                                            )}
                                        >
                                            <div className="mb-1 flex items-center gap-2">
                                                <div className="min-w-0 flex flex-1 items-center gap-1.5 overflow-hidden">
                                                    <span className="truncate text-sm font-semibold text-foreground">
                                                        {item.channel_name}
                                                    </span>
                                                    {item.model_name && (
                                                        <>
                                                            <span className="shrink-0 text-muted-foreground/40">/</span>
                                                            <span className="max-w-[40%] truncate text-xs text-muted-foreground">
                                                                {item.model_name}
                                                            </span>
                                                        </>
                                                    )}
                                                    {item.duration_ms > 0 && (
                                                        <span className="shrink-0 text-xs text-muted-foreground">
                                                            耗时 {formatDuration(item.duration_ms)}
                                                        </span>
                                                    )}
                                                </div>
                                                <StatusBadge status={item.status} />
                                            </div>
                                            <p className={cn(
                                                'max-w-full overflow-hidden text-ellipsis whitespace-nowrap line-clamp-1 text-xs leading-relaxed',
                                                item.error ? 'text-destructive' : 'text-muted-foreground'
                                            )}>
                                                {item.error || item.response_preview || '-'}
                                            </p>
                                        </button>
                                    ))}
                                </div>
                                <div className="border-t border-border/70 p-3">
                                    <Button
                                        type="button"
                                        onClick={handleSyncStatus}
                                        disabled={!taskId || syncTaskStatus.isPending}
                                        className="w-full rounded-xl"
                                    >
                                        {syncTaskStatus.isPending ? <Loader2 className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
                                        {syncStatusText}
                                    </Button>
                                </div>
                            </aside>

                            <section className="min-h-0 overflow-hidden rounded-3xl border border-border/70 bg-muted/20">
                                {!selectedItem && (
                                    <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                                        {emptyText}
                                    </div>
                                )}

                                {selectedItem && (
                                    <div className="flex h-full min-h-0 flex-col">
                                        <div className="border-b border-border/70 px-4 py-3 sm:py-4">
                                            <div className="flex items-center gap-2">
                                                <div className="min-w-0 flex-1">
                                                    <div className="flex flex-wrap items-center gap-2">
                                                        <h3 className="truncate text-base font-semibold text-foreground">{selectedItem.channel_name}</h3>
                                                        <StatusBadge status={selectedItem.status} />
                                                        <Badge variant="secondary" className="rounded-full px-2 py-0.5 text-xs">
                                                            {selectedItem.model_name}
                                                        </Badge>
                                                    </div>
                                                </div>
                                                <Button
                                                    type="button"
                                                    variant="ghost"
                                                    size="icon"
                                                    onClick={() => setDetailExpanded((value) => !value)}
                                                    className="size-7 rounded-lg text-muted-foreground"
                                                >
                                                    <ChevronDown className={cn('size-4 transition-transform', !detailExpanded && '-rotate-90')} />
                                                </Button>
                                            </div>

                                            {detailExpanded && (
                                                <div className="mt-3 space-y-3">
                                                    <div className="grid grid-cols-1 gap-2 text-xs text-muted-foreground sm:gap-3 md:grid-cols-2 2xl:grid-cols-4">
                                                        <div className="flex items-center gap-2">
                                                            <Server className="size-3.5 shrink-0" />
                                                            <span className="truncate">{selectedItem.request_url || selectedItem.base_url || '-'}</span>
                                                        </div>
                                                        {selectedItem.duration_ms > 0 && (
                                                            <div className="flex items-center gap-2">
                                                                <Clock className="size-3.5 shrink-0" />
                                                                <span>{formatDurationText(formatDuration(selectedItem.duration_ms))}</span>
                                                            </div>
                                                        )}
                                                        <div className="flex items-center gap-2">
                                                            <PlayCircle className="size-3.5 shrink-0" />
                                                            <span>{formatStatusCodeText(selectedItem.response_status_code || '-')}</span>
                                                        </div>
                                                        <div className="flex items-center gap-2">
                                                            <KeyRound className="size-3.5 shrink-0" />
                                                            <span className="truncate">
                                                                {selectedItem.channel_key_preview
                                                                    ? `${selectedItem.channel_key_index || '-'} · ${selectedItem.channel_key_preview}${selectedItem.channel_key_remark ? ` · ${selectedItem.channel_key_remark}` : ''}`
                                                                    : '-'}
                                                            </span>
                                                        </div>
                                                    </div>

                                                    <div className="border-t border-border/70 pt-3">
                                                        <div className="mb-2 text-xs font-medium text-foreground">{requestText}</div>
                                                        <pre className="max-h-40 overflow-auto rounded-xl border border-border/70 bg-background/70 p-3 text-xs leading-relaxed whitespace-pre-wrap break-all text-muted-foreground">
                                                            {selectedItem.request_content?.trim() ? selectedItem.request_content : '-'}
                                                        </pre>
                                                    </div>
                                                </div>
                                            )}
                                        </div>

                                        <div className="min-h-0 flex-1 overflow-auto p-3 sm:p-4">
                                            <div className="flex min-h-full flex-col">
                                                <PayloadPanel
                                                    title={responseText}
                                                    content={selectedItem.response_content}
                                                    className="flex-1"
                                                />
                                            </div>
                                        </div>
                                    </div>
                                )}
                            </section>
                        </div>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    );
}
