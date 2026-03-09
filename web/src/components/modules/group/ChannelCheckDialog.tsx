'use client';

import { useMemo, useState } from 'react';
import { Activity, AlertCircle, CheckCircle2, Clock, KeyRound, Loader2, PlayCircle, RefreshCw, Server, XCircle } from 'lucide-react';
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

function MetricCard({
    icon,
    label,
    value,
    className,
}: {
    icon: React.ReactNode;
    label: string;
    value: string | number;
    className?: string;
}) {
    return (
        <div className={cn('rounded-2xl border border-border/60 bg-background/80 p-3', className)}>
            <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
                {icon}
                <span>{label}</span>
            </div>
            <div className="text-lg font-semibold text-foreground">{value}</div>
        </div>
    );
}

function PayloadPanel({ title, content }: { title: string; content?: string }) {
    return (
        <section className="flex min-h-0 flex-col overflow-hidden rounded-2xl border border-border bg-muted/30">
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

    const effectiveSelectedItemId = useMemo(() => {
        if (items.length === 0) return null;
        return items.some((item) => item.id === selectedItemId) ? selectedItemId : (items[0]?.id ?? null);
    }, [items, selectedItemId]);

    const selectedItem = useMemo(() => {
        return items.find((item) => item.id === effectiveSelectedItemId) ?? items[0];
    }, [effectiveSelectedItemId, items]);

    const completedCount = (task?.success_count ?? 0) + (task?.failed_count ?? 0);
    const progressPercent = task?.total_count ? Math.round((completedCount / task.total_count) * 100) : 0;
    const progressLabel = task
        ? withTranslationFallback(
            t('progress', { done: completedCount, total: task.total_count }),
            `${completedCount} / ${task.total_count} 已完成`,
            ['group.healthCheck.progress', 'progress']
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
    const metricsTotalText = withTranslationFallback(t('metrics.total'), '总数', ['group.healthCheck.metrics.total', 'metrics.total']);
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

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent
                showCloseButton={false}
                className="h-[calc(100vh-2rem)] w-[calc(100vw-2rem)] max-w-[calc(100vw-2rem)] sm:max-w-[calc(100vw-2rem)] lg:h-[calc(100vh-3rem)] lg:w-[min(1400px,96vw)] lg:max-w-[min(1400px,96vw)] rounded-3xl border-border/70 bg-card px-5 py-4 text-card-foreground custom-shadow flex flex-col overflow-hidden"
            >
                <DialogClose asChild>
                    <Button variant="ghost" size="icon" className="absolute right-4 top-4 rounded-xl text-muted-foreground hover:text-foreground">
                        <XCircle className="size-4" />
                    </Button>
                </DialogClose>

                <div className="mb-4 pr-12">
                    <div className="mb-2 flex items-center gap-3">
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
                            <StatusBadge status={task?.status} />
                        </div>
                    </div>

                    {task && (
                        <>
                            <div className="mb-3 grid grid-cols-2 gap-3 lg:grid-cols-5">
                                <MetricCard icon={<PlayCircle className="size-3.5 text-primary" />} label={metricsTotalText} value={task.total_count} />
                                <MetricCard icon={<Loader2 className="size-3.5 text-primary" />} label={metricsRunningText} value={task.running_count} />
                                <MetricCard icon={<CheckCircle2 className="size-3.5 text-emerald-500" />} label={metricsSuccessText} value={task.success_count} />
                                <MetricCard icon={<AlertCircle className="size-3.5 text-destructive" />} label={metricsFailedText} value={task.failed_count} />
                                <MetricCard icon={<Clock className="size-3.5 text-muted-foreground" />} label={metricsCreatedAtText} value={formatTaskTime(task.created_at)} />
                            </div>

                            <div className="rounded-2xl border border-border/70 bg-background/70 p-3">
                                <div className="mb-2 flex items-center justify-between gap-3 text-xs text-muted-foreground">
                                    <span>{progressLabel}</span>
                                    <span>{progressPercent}%</span>
                                </div>
                                <div className="h-2 overflow-hidden rounded-full bg-muted">
                                    <div
                                        className="h-full rounded-full bg-linear-to-r from-primary to-emerald-500 transition-all duration-300"
                                        style={{ width: `${progressPercent}%` }}
                                    />
                                </div>
                            </div>
                        </>
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
                        <div className="grid h-full min-h-0 grid-cols-1 gap-4 xl:grid-cols-[340px_minmax(0,1fr)]">
                            <aside className="flex min-h-0 flex-col overflow-hidden rounded-3xl border border-border/70 bg-muted/20">
                                <div className="border-b border-border/70 px-4 py-3 text-sm font-medium text-foreground">
                                    {resultListText}
                                </div>
                                <div className="min-h-0 flex-1 overflow-auto p-2">
                                    {items.map((item) => (
                                        <button
                                            key={item.id}
                                            type="button"
                                            onClick={() => setSelectedItemId(item.id)}
                                            className={cn(
                                                'rounded-2xl border px-3 py-3 text-left transition-colors',
                                                selectedItem?.id === item.id
                                                    ? 'border-primary/40 bg-primary/5'
                                                    : 'border-transparent hover:border-border hover:bg-background/70'
                                            )}
                                        >
                                            <div className="mb-1 flex items-start gap-2">
                                                <div className="min-w-0 flex-1">
                                                    <div className="truncate text-sm font-semibold text-foreground">
                                                        {item.channel_name}
                                                    </div>
                                                    <div className="truncate text-xs text-muted-foreground">
                                                        {item.model_name}
                                                    </div>
                                                </div>
                                                <StatusBadge status={item.status} />
                                            </div>
                                            <div className="space-y-1 text-xs text-muted-foreground">
                                                <div className="flex items-center gap-1.5">
                                                    <Clock className="size-3.5 shrink-0" />
                                                    <span>{formatDuration(item.duration_ms)}</span>
                                                </div>
                                                {(item.error || item.response_preview) && (
                                                    <p className={cn(
                                                        'line-clamp-2 leading-relaxed',
                                                        item.error ? 'text-destructive' : 'text-muted-foreground'
                                                    )}>
                                                        {item.error || item.response_preview}
                                                    </p>
                                                )}
                                            </div>
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
                                        <div className="border-b border-border/70 px-4 py-4">
                                            <div className="mb-2 flex flex-wrap items-center gap-2">
                                                <h3 className="text-base font-semibold text-foreground">{selectedItem.channel_name}</h3>
                                                <StatusBadge status={selectedItem.status} />
                                                <Badge variant="secondary" className="rounded-full px-2 py-0.5 text-xs">
                                                    {selectedItem.model_name}
                                                </Badge>
                                            </div>

                                        <div className="grid grid-cols-1 gap-3 text-xs text-muted-foreground md:grid-cols-2 2xl:grid-cols-4">
                                                <div className="flex items-center gap-2">
                                                    <Server className="size-3.5 shrink-0" />
                                                    <span className="truncate">{selectedItem.request_url || selectedItem.base_url || '-'}</span>
                                                </div>
                                                <div className="flex items-center gap-2">
                                                    <Clock className="size-3.5 shrink-0" />
                                                    <span>{formatDurationText(formatDuration(selectedItem.duration_ms))}</span>
                                                </div>
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
                                        </div>

                                        <div className="min-h-0 flex-1 overflow-auto p-4">
                                            <div className="flex min-h-full flex-col gap-2">
                                                <PayloadPanel title={requestText} content={selectedItem.request_content} />
                                                <PayloadPanel title={responseText} content={selectedItem.response_content} />
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
