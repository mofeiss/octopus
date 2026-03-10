'use client';

import { useMemo, useState } from 'react';
import { Activity, AlertCircle, CheckCircle2, Clock, KeyRound, Loader2, RefreshCw, XIcon } from 'lucide-react';
import { Claude, OpenAI } from '@lobehub/icons';
import { useTranslations } from 'next-intl';
import {
    type GroupChannelCheckAttempt,
    type GroupChannelCheckTaskItem,
    GroupChannelCheckItemStatus,
    GroupChannelCheckTaskStatus,
    useSyncGroupChannelCheckTaskStatus,
    useGroupChannelCheckTaskDetail,
} from '@/api/endpoints/group-channel-check';
import { CopyIconButton } from '@/components/common/CopyButton';
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

function buildFallbackAttempt(item: GroupChannelCheckTaskItem): GroupChannelCheckAttempt {
    return {
        status: item.status,
        channel_key_id: item.channel_key_id,
        channel_key_index: item.channel_key_index,
        channel_key_preview: item.channel_key_preview,
        channel_key_remark: item.channel_key_remark,
        response_status_code: item.response_status_code,
        openai_request_curl: item.openai_request_curl,
        anthropic_request_curl: item.anthropic_request_curl,
        response_content: item.response_content?.trim() || item.error || item.response_preview || '-',
        error: item.error,
    };
}

function getAttemptTone(status?: GroupChannelCheckItemStatus) {
    switch (status) {
        case GroupChannelCheckItemStatus.Success:
            return {
                card: 'border-emerald-500/25 bg-emerald-500/5',
                divider: 'border-emerald-500/20',
                accent: 'text-emerald-600',
                body: 'text-foreground',
            };
        case GroupChannelCheckItemStatus.Failed:
            return {
                card: 'border-destructive/25 bg-destructive/5',
                divider: 'border-destructive/20',
                accent: 'text-destructive',
                body: 'text-destructive',
            };
        case GroupChannelCheckItemStatus.Running:
            return {
                card: 'border-primary/25 bg-primary/5',
                divider: 'border-primary/20',
                accent: 'text-primary',
                body: 'text-foreground',
            };
        default:
            return {
                card: 'border-border/70 bg-background/60',
                divider: 'border-border/70',
                accent: 'text-muted-foreground',
                body: 'text-muted-foreground',
            };
    }
}

function ChannelKeyBadge({
    index,
    remark,
    className,
}: {
    index?: number;
    remark?: string;
    className?: string;
}) {
    if (!(index && index > 0) && !remark?.trim()) return null;

    return (
        <Badge
            variant="secondary"
            className={cn('shrink-0 text-xs px-1.5 py-0 inline-flex items-center gap-0.5', className)}
            title={remark?.trim() ? `Key ${index || '-'} · ${remark.trim()}` : `Key ${index || '-'}`}
        >
            <KeyRound className="size-3" />
            <span>{index || '-'}</span>
            {remark?.trim() && (
                <>
                    <span className="text-muted-foreground/50">/</span>
                    <span className="max-w-24 truncate">
                        {remark.trim()}
                    </span>
                </>
            )}
        </Badge>
    );
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

export function GroupChannelCheckDialog({
    open,
    onOpenChange,
    taskId,
    creating,
    onRetry,
    retrying,
    onRetrySelected,
    retryingSelected,
}: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    taskId?: number | null;
    creating?: boolean;
    onRetry?: () => void;
    retrying?: boolean;
    onRetrySelected?: (item: GroupChannelCheckTaskItem) => void;
    retryingSelected?: boolean;
}) {
    const t = useTranslations('group.healthCheck');
    const taskQuery = useGroupChannelCheckTaskDetail(taskId ?? undefined, open && !!taskId);
    const syncTaskStatus = useSyncGroupChannelCheckTaskStatus();
    const task = taskQuery.data;
    const items = useMemo(() => task?.items ?? [], [task?.items]);
    const [selectedItemId, setSelectedItemId] = useState<number | null>(null);

    const effectiveSelectedItemId = useMemo(() => {
        if (items.length === 0) return null;
        return items.some((item) => item.id === selectedItemId) ? selectedItemId : null;
    }, [items, selectedItemId]);

    const sortedItems = useMemo(() => {
        const statusRank = (status?: GroupChannelCheckItemStatus) => {
            switch (status) {
                case GroupChannelCheckItemStatus.Success:
                    return 0;
                case GroupChannelCheckItemStatus.Running:
                    return 1;
                case GroupChannelCheckItemStatus.Pending:
                    return 2;
                case GroupChannelCheckItemStatus.Failed:
                    return 3;
                default:
                    return 4;
            }
        };

        return [...items].sort((a, b) => {
            const rankDiff = statusRank(a.status) - statusRank(b.status);
            if (rankDiff !== 0) return rankDiff;
            return a.id - b.id;
        });
    }, [items]);

    const selectedItem = useMemo(() => {
        return sortedItems.find((item) => item.id === effectiveSelectedItemId) ?? sortedItems[0];
    }, [effectiveSelectedItemId, sortedItems]);
    const selectedItemAttempts = useMemo(() => {
        if (!selectedItem) return [];
        return selectedItem.attempts && selectedItem.attempts.length > 0
            ? selectedItem.attempts
            : [buildFallbackAttempt(selectedItem)];
    }, [selectedItem]);

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
    const apiAddressText = withTranslationFallback(t('detail.apiAddress'), 'API 地址', ['group.healthCheck.detail.apiAddress', 'detail.apiAddress']);
    const requestText = withTranslationFallback(t('detail.request'), '请求内容', ['group.healthCheck.detail.request', 'detail.request']);
    const metricsRunningText = withTranslationFallback(t('metrics.running'), '进行中', ['group.healthCheck.metrics.running', 'metrics.running']);
    const metricsSuccessText = withTranslationFallback(t('metrics.success'), '成功', ['group.healthCheck.metrics.success', 'metrics.success']);
    const metricsFailedText = withTranslationFallback(t('metrics.failed'), '失败', ['group.healthCheck.metrics.failed', 'metrics.failed']);
    const metricsCreatedAtText = withTranslationFallback(t('metrics.createdAt'), '创建时间', ['group.healthCheck.metrics.createdAt', 'metrics.createdAt']);

    const syncStatusText = withTranslationFallback(
        t('actions.syncStatus'),
        '按测活结果同步启用状态',
        ['group.healthCheck.actions.syncStatus', 'actions.syncStatus']
    );
    const retrySelectedText = withTranslationFallback(
        t('actions.retrySelected'),
        '再测一次',
        ['group.healthCheck.actions.retrySelected', 'actions.retrySelected']
    );
    const retryAllText = withTranslationFallback(
        t('actions.retryAll'),
        '全部重测',
        ['group.healthCheck.actions.retryAll', 'actions.retryAll']
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
                <div className="mb-0">
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
                                <DialogClose asChild>
                                    <Button
                                        variant="ghost"
                                        size="icon"
                                        className="size-8 rounded-xl text-muted-foreground transition-colors hover:text-foreground"
                                    >
                                        <XIcon className="size-6" />
                                    </Button>
                                </DialogClose>
                            </div>
                        </div>
                    </div>

                    {task && (
                        <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 pl-0 pr-1 text-[11px] text-muted-foreground sm:pl-[3.25rem] sm:text-xs">
                            <div className="flex items-center gap-1.5 whitespace-nowrap">
                                <CheckCircle2 className="size-3.5 shrink-0 text-emerald-500" />
                                <span>{metricsSuccessText} {task.success_count}</span>
                            </div>
                            <div className="flex items-center gap-1.5 whitespace-nowrap">
                                <AlertCircle className="size-3.5 shrink-0 text-destructive" />
                                <span>{metricsFailedText} {task.failed_count}</span>
                            </div>
                            <div className="flex items-center gap-1.5 whitespace-nowrap">
                                <Loader2 className="size-3.5 shrink-0 text-primary" />
                                <span>{metricsRunningText} {task.running_count}</span>
                            </div>
                            <div className="flex items-center gap-1.5 whitespace-nowrap">
                                <Clock className="size-3.5 shrink-0 text-muted-foreground" />
                                <span>{metricsCreatedAtText} {formatTaskTime(task.created_at)}</span>
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
                        <div className="grid h-full min-h-0 grid-cols-1 grid-rows-[minmax(0,0.95fr)_minmax(0,1.05fr)] gap-3 xl:grid-cols-[340px_minmax(0,1fr)] xl:grid-rows-1 xl:gap-4">
                            <aside className="flex min-h-0 flex-col overflow-hidden rounded-3xl border border-border/70 bg-muted/20">
                                <div className="border-b border-border/70 px-4 py-3 text-sm font-medium text-foreground">
                                    {resultListText}
                                </div>
                                <div className="min-h-0 flex-1 overflow-auto p-2 space-y-2">
                                    {sortedItems.map((item) => (
                                        <button
                                            key={item.id}
                                            type="button"
                                            onClick={() => {
                                                setSelectedItemId(item.id);
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
                                                </div>
                                                <div className="ml-auto flex shrink-0 items-center gap-1.5">
                                                    {item.duration_ms > 0 && (
                                                        <Badge
                                                            variant="outline"
                                                            className="rounded-full border border-border/70 px-2.5 py-1 text-xs font-medium text-muted-foreground"
                                                        >
                                                            {formatDuration(item.duration_ms)}
                                                        </Badge>
                                                    )}
                                                    <ChannelKeyBadge
                                                        index={item.channel_key_index}
                                                        remark={item.channel_key_remark}
                                                        className="max-w-[10rem]"
                                                    />
                                                    <StatusBadge status={item.status} />
                                                </div>
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
                                <div className="border-t border-border/70 px-4 py-3">
                                    <div className="flex gap-2">
                                        <Button
                                            type="button"
                                            onClick={handleSyncStatus}
                                            disabled={!taskId || syncTaskStatus.isPending}
                                            className="h-8 min-w-0 flex-1 rounded-xl px-3 text-xs"
                                        >
                                            {syncTaskStatus.isPending ? <Loader2 className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
                                            {syncStatusText}
                                        </Button>
                                        <Button
                                            type="button"
                                            variant="secondary"
                                            onClick={onRetry}
                                            disabled={!onRetry || retrying}
                                            className="h-8 shrink-0 rounded-xl px-3 text-xs"
                                        >
                                            {retrying ? <Loader2 className="size-4 animate-spin" /> : <Activity className="size-4" />}
                                            {retryAllText}
                                        </Button>
                                    </div>
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
                                                        {selectedItem.model_name && (
                                                            <Badge variant="secondary" className="rounded-full px-2 py-0.5 text-xs">
                                                                {selectedItem.model_name}
                                                            </Badge>
                                                        )}
                                                    </div>
                                                </div>
                                                <Button
                                                    type="button"
                                                    variant="secondary"
                                                    onClick={() => onRetrySelected?.(selectedItem)}
                                                    disabled={!onRetrySelected || retryingSelected}
                                                    className="h-8 rounded-xl px-3 text-xs"
                                                >
                                                    {retryingSelected ? <Loader2 className="size-3.5 animate-spin" /> : <Activity className="size-3.5" />}
                                                    {retrySelectedText}
                                                </Button>
                                            </div>

                                            <div className="mt-3 space-y-2 border-t border-border/70 pt-3 text-xs">
                                                <div className="flex items-center gap-3">
                                                    <span className="shrink-0 text-muted-foreground">{apiAddressText}</span>
                                                    <div className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap text-foreground [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
                                                        {selectedItem.request_url || selectedItem.base_url || '-'}
                                                    </div>
                                                </div>
                                                <div className="flex items-center gap-3">
                                                    <span className="shrink-0 text-muted-foreground">{requestText}</span>
                                                    <div className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap text-foreground [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
                                                        {selectedItem.request_content?.trim() ? selectedItem.request_content : '-'}
                                                    </div>
                                                    </div>
                                                </div>
                                            </div>

                                        <div className="min-h-0 flex-1 overflow-auto p-3 sm:p-4">
                                            <div className="flex min-h-full flex-col gap-3">
                                                {selectedItemAttempts.map((attempt, index) => {
                                                    const tone = getAttemptTone(attempt.status);
                                                    const responseContent = attempt.response_content?.trim() || attempt.error || '-';

                                                    return (
                                                        <div
                                                            key={`${selectedItem.id}-${attempt.channel_key_id ?? attempt.channel_key_index ?? index}-${index}`}
                                                            className={cn('overflow-hidden rounded-2xl border', tone.card)}
                                                        >
                                                            <div className={cn('flex items-center gap-2 px-3 py-2 text-xs', tone.divider, 'border-b')}>
                                                                <span className={cn('shrink-0 font-semibold', tone.accent)}>
                                                                    {attempt.response_status_code || '-'}
                                                                </span>
                                                                <span className="shrink-0 text-muted-foreground/40">/</span>
                                                                <ChannelKeyBadge
                                                                    index={attempt.channel_key_index}
                                                                    remark={attempt.channel_key_remark}
                                                                    className="max-w-[12rem]"
                                                                />
                                                                <span className="shrink-0 text-muted-foreground/40">/</span>
                                                                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                                                                    {attempt.channel_key_preview || '-'}
                                                                </span>
                                                                {attempt.openai_request_curl?.trim() && (
                                                                    <CopyIconButton
                                                                        text={attempt.openai_request_curl.trim()}
                                                                        title="复制 OpenAI 兼容 curl"
                                                                        icon={<OpenAI.Avatar size={14} />}
                                                                        className="shrink-0 rounded-lg p-1 text-muted-foreground transition-colors hover:bg-muted/80 hover:text-foreground"
                                                                        checkIconClassName="size-3.5 text-primary"
                                                                    />
                                                                )}
                                                                {attempt.anthropic_request_curl?.trim() && (
                                                                    <CopyIconButton
                                                                        text={attempt.anthropic_request_curl.trim()}
                                                                        title="复制 Anthropic 兼容 curl"
                                                                        icon={<Claude.Avatar size={14} />}
                                                                        className="shrink-0 rounded-lg p-1 text-muted-foreground transition-colors hover:bg-muted/80 hover:text-foreground"
                                                                        checkIconClassName="size-3.5 text-primary"
                                                                    />
                                                                )}
                                                            </div>
                                                            <div className="flex items-start gap-3 px-3 py-3">
                                                                <pre className={cn('min-w-0 flex-1 whitespace-pre-wrap break-all text-xs leading-relaxed', tone.body)}>
                                                                    {responseContent}
                                                                </pre>
                                                            </div>
                                                        </div>
                                                    );
                                                })}
                                                {selectedItemAttempts.length === 0 && (
                                                    <div className="flex min-h-[120px] items-center justify-center rounded-2xl border border-border/70 bg-background/60 text-sm text-muted-foreground">
                                                        {emptyText}
                                                    </div>
                                                )}
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
