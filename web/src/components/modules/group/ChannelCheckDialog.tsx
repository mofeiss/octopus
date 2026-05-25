'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Activity, AlertCircle, CheckCircle2, KeyRound, Loader2, XIcon } from 'lucide-react';
import { Claude, OpenAI } from '@lobehub/icons';
import { useTranslations } from 'next-intl';
import {
    type GroupChannelCheckAttempt,
    type GroupChannelCheckTaskItem,
    GroupChannelCheckItemStatus,
    GroupChannelCheckProtocol,
    useProbeGroupChannelCheck,
} from '@/api/endpoints/group-channel-check';
import type { Group, GroupItem } from '@/api/endpoints/group';
import { useModelChannelList } from '@/api/endpoints/model';
import { CopyIconButton } from '@/components/common/CopyButton';
import { toast } from '@/components/common/Toast';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { cn } from '@/lib/utils';
import { buildChannelNameByModelKey, modelChannelKey } from './utils';

export type ChannelCheckSelection = {
    groupId?: number;
    groupItemId?: number;
    channelId?: number;
    modelName?: string;
};

type ChannelCheckTarget = {
    key: string;
    groupId: number;
    groupName: string;
    groupItemId?: number;
    channelId: number;
    channelName: string;
    modelName: string;
    item: GroupItem;
};

const CHECK_PROTOCOL_OPTIONS = [
    { value: GroupChannelCheckProtocol.OpenAIChat, label: 'OPENAI CHAT' },
    { value: GroupChannelCheckProtocol.OpenAIResponse, label: 'OPENAI RESPONSE' },
    { value: GroupChannelCheckProtocol.Anthropic, label: 'ANTHROPIC' },
] as const;

function withTranslationFallback(translated: string, fallback: string, keys: string[]) {
    const normalized = translated.trim();
    if (!normalized) return fallback;
    if (normalized.startsWith('MISSING_MESSAGE') || normalized.startsWith('MISSING_TRANSLATION')) return fallback;
    if (keys.includes(normalized)) return fallback;
    if (keys.some((key) => normalized.endsWith(key))) return fallback;
    return translated;
}

function formatDuration(durationMs?: number) {
    if (!durationMs) return '-';
    if (durationMs < 1000) return `${durationMs}ms`;
    return `${(durationMs / 1000).toFixed(2)}s`;
}

function buildTargetKey(groupId: number, item: Pick<GroupItem, 'id' | 'channel_id' | 'model_name'>) {
    return `${groupId}:${item.id ?? 'new'}:${item.channel_id}:${item.model_name}`;
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
        default:
            return {
                card: 'border-border/70 bg-background/60',
                divider: 'border-border/70',
                accent: 'text-muted-foreground',
                body: 'text-muted-foreground',
            };
    }
}

function getTargetStatus(target: ChannelCheckTarget, result?: GroupChannelCheckTaskItem) {
    const status = result?.status ?? target.item.health_check_status;
    if (status === GroupChannelCheckItemStatus.Success) return GroupChannelCheckItemStatus.Success;
    if (status === GroupChannelCheckItemStatus.Failed) return GroupChannelCheckItemStatus.Failed;
    return undefined;
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

function ResultBadge({ status }: { status?: GroupChannelCheckItemStatus }) {
    const t = useTranslations('group.healthCheck');

    if (status === GroupChannelCheckItemStatus.Success) {
        return (
            <Badge variant="outline" className="rounded-full border-emerald-500/20 bg-emerald-500/10 px-2.5 py-1 text-xs font-medium text-emerald-600">
                {withTranslationFallback(t('status.success'), '测试成功', ['group.healthCheck.status.success', 'status.success'])}
            </Badge>
        );
    }

    if (status === GroupChannelCheckItemStatus.Failed) {
        return (
            <Badge variant="outline" className="rounded-full border-destructive/20 bg-destructive/10 px-2.5 py-1 text-xs font-medium text-destructive">
                {withTranslationFallback(t('status.failed'), '测试失败', ['group.healthCheck.status.failed', 'status.failed'])}
            </Badge>
        );
    }

    return (
        <Badge variant="outline" className="rounded-full border-border bg-muted px-2.5 py-1 text-xs font-medium text-muted-foreground">
            {withTranslationFallback(t('status.untested'), '未测试', ['group.healthCheck.status.untested', 'status.untested'])}
        </Badge>
    );
}

function findSelectionKey(targets: ChannelCheckTarget[], selection?: ChannelCheckSelection | null) {
    if (targets.length === 0) return null;
    if (!selection) return targets[0].key;

    if (selection.groupItemId) {
        const exact = targets.find((target) => target.groupItemId === selection.groupItemId);
        if (exact) return exact.key;
    }

    if (selection.groupId && selection.channelId && selection.modelName) {
        const exact = targets.find((target) =>
            target.groupId === selection.groupId
            && target.channelId === selection.channelId
            && target.modelName === selection.modelName
        );
        if (exact) return exact.key;
    }

    if (selection.groupId) {
        const firstInGroup = targets.find((target) => target.groupId === selection.groupId);
        if (firstInGroup) return firstInGroup.key;
    }

    return targets[0].key;
}

export function GroupChannelCheckDialog({
    open,
    onOpenChange,
    groups,
    initialSelection,
}: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    groups: Group[];
    initialSelection?: ChannelCheckSelection | null;
}) {
    const t = useTranslations('group.healthCheck');
    const { data: modelChannels = [] } = useModelChannelList();
    const probeGroupChannelCheck = useProbeGroupChannelCheck();
    const [selectedKey, setSelectedKey] = useState<string | null>(null);
    const [selectedProtocol, setSelectedProtocol] = useState<GroupChannelCheckProtocol>(GroupChannelCheckProtocol.OpenAIChat);
    const [resultByKey, setResultByKey] = useState<Record<string, GroupChannelCheckTaskItem>>({});
    const [runningKey, setRunningKey] = useState<string | null>(null);
    const wasOpenRef = useRef(false);
    const appliedSelectionSignatureRef = useRef('');

    const channelNameByKey = useMemo(() => buildChannelNameByModelKey(modelChannels), [modelChannels]);

    const groupedTargets = useMemo(() => {
        return [...groups]
            .filter((group): group is Group & { id: number } => typeof group.id === 'number')
            .sort((a, b) => (a.sort_order || a.id) - (b.sort_order || b.id))
            .map((group) => {
                const targets = [...(group.items ?? [])]
                    .sort((a, b) => a.priority - b.priority)
                    .map((item) => ({
                        key: buildTargetKey(group.id, item),
                        groupId: group.id,
                        groupName: group.name,
                        groupItemId: item.id,
                        channelId: item.channel_id,
                        channelName: channelNameByKey.get(modelChannelKey(item.channel_id, item.model_name)) ?? `Channel ${item.channel_id}`,
                        modelName: item.model_name,
                        item,
                    }));
                return { group, targets };
            });
    }, [channelNameByKey, groups]);

    const targets = useMemo(() => groupedTargets.flatMap((group) => group.targets), [groupedTargets]);
    const selectedTarget = useMemo(
        () => targets.find((target) => target.key === selectedKey) ?? targets[0] ?? null,
        [selectedKey, targets]
    );
    const selectedResult = selectedTarget ? resultByKey[selectedTarget.key] : undefined;
    const selectedAttempts = useMemo(() => {
        if (!selectedResult) return [];
        return selectedResult.attempts && selectedResult.attempts.length > 0
            ? selectedResult.attempts
            : [buildFallbackAttempt(selectedResult)];
    }, [selectedResult]);

    const titleText = withTranslationFallback(t('title'), '渠道测活', ['group.healthCheck.title', 'title']);
    const channelListText = withTranslationFallback(t('channelList'), '渠道列表', ['group.healthCheck.channelList', 'channelList']);
    const emptyText = withTranslationFallback(t('empty'), '暂无渠道', ['group.healthCheck.empty', 'empty']);
    const apiAddressText = withTranslationFallback(t('detail.apiAddress'), 'API 地址', ['group.healthCheck.detail.apiAddress', 'detail.apiAddress']);
    const requestText = withTranslationFallback(t('detail.request'), '请求内容', ['group.healthCheck.detail.request', 'detail.request']);
    const responseText = withTranslationFallback(t('detail.response'), '响应内容', ['group.healthCheck.detail.response', 'detail.response']);
    const sendText = withTranslationFallback(t('actions.send'), '发送测试', ['group.healthCheck.actions.send', 'actions.send']);
    const sendSuccessText = withTranslationFallback(t('toast.sent'), '测试已完成', ['group.healthCheck.toast.sent', 'toast.sent']);
    const sendFailedText = withTranslationFallback(t('toast.failed'), '发送测试失败', ['group.healthCheck.toast.failed', 'toast.failed']);
    const initialSelectionSignature = `${initialSelection?.groupId ?? ''}:${initialSelection?.groupItemId ?? ''}:${initialSelection?.channelId ?? ''}:${initialSelection?.modelName ?? ''}`;

    useEffect(() => {
        if (!open) {
            wasOpenRef.current = false;
            return;
        }

        const selectedStillExists = !!selectedKey && targets.some((target) => target.key === selectedKey);
        const selectionChanged = appliedSelectionSignatureRef.current !== initialSelectionSignature;
        if (!wasOpenRef.current || selectionChanged || !selectedStillExists) {
            const nextKey = findSelectionKey(targets, initialSelection);
            setSelectedKey(nextKey);
            appliedSelectionSignatureRef.current = initialSelectionSignature;
        }
        wasOpenRef.current = true;
    }, [initialSelection, initialSelectionSignature, open, selectedKey, targets]);

    const handleDialogOpenChange = (nextOpen: boolean) => {
        if (!nextOpen) {
            setRunningKey(null);
        }
        onOpenChange(nextOpen);
    };

    const handleSendTest = useCallback(async () => {
        if (!selectedTarget || runningKey) return;

        setRunningKey(selectedTarget.key);
        try {
            const result = await probeGroupChannelCheck.mutateAsync({
                group_id: selectedTarget.groupId,
                group_item_id: selectedTarget.groupItemId,
                channel_id: selectedTarget.channelId,
                model_name: selectedTarget.modelName,
                protocol: selectedProtocol,
            });
            setResultByKey((prev) => ({ ...prev, [selectedTarget.key]: result }));
            toast.success(sendSuccessText);
        } catch (error) {
            const message = error instanceof Error ? error.message : String(error);
            toast.error(sendFailedText, { description: message });
        } finally {
            setRunningKey(null);
        }
    }, [probeGroupChannelCheck, runningKey, selectedProtocol, selectedTarget, sendFailedText, sendSuccessText]);

    return (
        <Dialog open={open} onOpenChange={handleDialogOpenChange}>
            <DialogContent
                showCloseButton={false}
                className="h-[calc(100vh-2rem)] w-[calc(100vw-2rem)] max-w-[calc(100vw-2rem)] sm:max-w-[calc(100vw-2rem)] lg:h-[calc(100vh-3rem)] lg:w-[min(1320px,96vw)] lg:max-w-[min(1320px,96vw)] rounded-2xl border-border/70 bg-card px-5 py-4 text-card-foreground custom-shadow flex flex-col overflow-hidden"
            >
                <div className="mb-3 flex items-center gap-3">
                    <div className="flex size-10 items-center justify-center rounded-xl bg-primary/10 text-primary">
                        <Activity className="size-5" />
                    </div>
                    <div className="min-w-0">
                        <DialogTitle className="truncate text-xl font-bold">{titleText}</DialogTitle>
                        <DialogDescription className="sr-only">{titleText}</DialogDescription>
                    </div>
                    <DialogClose asChild>
                        <Button
                            variant="ghost"
                            size="icon"
                            className="ml-auto size-8 rounded-xl text-muted-foreground transition-colors hover:text-foreground"
                        >
                            <XIcon className="size-6" />
                        </Button>
                    </DialogClose>
                </div>

                <div className="grid min-h-0 flex-1 grid-cols-1 grid-rows-[minmax(0,0.95fr)_minmax(0,1.05fr)] gap-3 xl:grid-cols-[360px_minmax(0,1fr)] xl:grid-rows-1 xl:gap-4">
                    <aside className="flex min-h-0 flex-col overflow-hidden rounded-2xl border border-border/70 bg-muted/20">
                        <div className="border-b border-border/70 px-4 py-3 text-sm font-medium text-foreground">
                            {channelListText}
                        </div>
                        <div className="min-h-0 flex-1 overflow-auto p-2">
                            {groupedTargets.length === 0 && (
                                <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                                    {emptyText}
                                </div>
                            )}

                            <div className="space-y-3">
                                {groupedTargets.map(({ group, targets: groupTargets }) => (
                                    <section key={group.id} className="min-w-0">
                                        <div className="mb-1.5 truncate px-2 text-xs font-semibold text-muted-foreground">
                                            {group.name}
                                        </div>
                                        <div className="space-y-1.5">
                                            {groupTargets.map((target) => {
                                                const result = resultByKey[target.key];
                                                const status = getTargetStatus(target, result);
                                                const isSelected = selectedTarget?.key === target.key;
                                                const summary = result?.error || result?.response_preview || target.item.health_check_error || '';

                                                return (
                                                    <button
                                                        key={target.key}
                                                        type="button"
                                                        onClick={() => setSelectedKey(target.key)}
                                                        className={cn(
                                                            'w-full min-w-0 overflow-hidden rounded-xl border px-3 py-2.5 text-left transition-colors',
                                                            isSelected
                                                                ? 'border-primary/40 bg-primary/5 shadow-xs'
                                                                : 'border-border/70 bg-background/60 hover:border-border hover:bg-background/80'
                                                        )}
                                                    >
                                                        <div className="mb-1 flex min-w-0 items-center gap-2">
                                                            <div className="min-w-0 flex-1">
                                                                <div className="truncate text-sm font-semibold text-foreground">
                                                                    {target.channelName}
                                                                </div>
                                                                <div className="truncate text-xs text-muted-foreground">
                                                                    {target.modelName}
                                                                </div>
                                                            </div>
                                                            <ResultBadge status={status} />
                                                        </div>
                                                        <div className={cn(
                                                            'truncate text-xs',
                                                            status === GroupChannelCheckItemStatus.Failed ? 'text-destructive' : 'text-muted-foreground'
                                                        )}>
                                                            {summary || '-'}
                                                        </div>
                                                    </button>
                                                );
                                            })}
                                        </div>
                                    </section>
                                ))}
                            </div>
                        </div>
                    </aside>

                    <section className="min-h-0 overflow-hidden rounded-2xl border border-border/70 bg-muted/20">
                        {!selectedTarget && (
                            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                                {emptyText}
                            </div>
                        )}

                        {selectedTarget && (
                            <div className="flex h-full min-h-0 flex-col">
                                <div className="border-b border-border/70 px-4 py-3 sm:py-4">
                                    <div className="flex flex-wrap items-center gap-2">
                                        <div className="min-w-0 flex-1">
                                            <div className="flex flex-wrap items-center gap-2">
                                                <h3 className="max-w-full truncate text-base font-semibold text-foreground">
                                                    {selectedTarget.channelName}
                                                </h3>
                                                <Badge variant="secondary" className="max-w-full truncate rounded-full px-2 py-0.5 text-xs">
                                                    {selectedTarget.groupName}
                                                </Badge>
                                                <Badge variant="outline" className="max-w-full truncate rounded-full border-border/70 px-2 py-0.5 text-xs text-muted-foreground">
                                                    {selectedTarget.modelName}
                                                </Badge>
                                            </div>
                                        </div>

                                        <div className="flex shrink-0 items-center gap-2">
                                            <Select
                                                value={selectedProtocol}
                                                onValueChange={(value) => setSelectedProtocol(value as GroupChannelCheckProtocol)}
                                            >
                                                <SelectTrigger className="h-8 w-[10.75rem] rounded-xl px-2.5 text-xs">
                                                    <SelectValue />
                                                </SelectTrigger>
                                                <SelectContent>
                                                    {CHECK_PROTOCOL_OPTIONS.map((option) => (
                                                        <SelectItem key={option.value} value={option.value}>
                                                            {option.label}
                                                        </SelectItem>
                                                    ))}
                                                </SelectContent>
                                            </Select>
                                            <Button
                                                type="button"
                                                variant="secondary"
                                                onClick={handleSendTest}
                                                disabled={!!runningKey}
                                                className="h-8 rounded-xl px-3 text-xs"
                                            >
                                                {runningKey === selectedTarget.key ? <Loader2 className="size-3.5 animate-spin" /> : <Activity className="size-3.5" />}
                                                {sendText}
                                            </Button>
                                        </div>
                                    </div>

                                    <div className="mt-3 grid gap-2 border-t border-border/70 pt-3 text-xs">
                                        <div className="flex min-w-0 items-center gap-3">
                                            <span className="shrink-0 text-muted-foreground">{apiAddressText}</span>
                                            <div className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap text-foreground [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
                                                {selectedResult?.request_url || selectedResult?.base_url || '-'}
                                            </div>
                                        </div>
                                        <div className="flex min-w-0 items-center gap-3">
                                            <span className="shrink-0 text-muted-foreground">{requestText}</span>
                                            <div className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap text-foreground [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
                                                {selectedResult?.request_content?.trim() ? selectedResult.request_content : '-'}
                                            </div>
                                        </div>
                                        {selectedResult && (
                                            <div className="flex flex-wrap items-center gap-2">
                                                <ResultBadge status={selectedResult.status} />
                                                {selectedResult.duration_ms > 0 && (
                                                    <Badge variant="outline" className="rounded-full border-border/70 px-2.5 py-1 text-xs font-medium text-muted-foreground">
                                                        {formatDuration(selectedResult.duration_ms)}
                                                    </Badge>
                                                )}
                                                <ChannelKeyBadge
                                                    index={selectedResult.channel_key_index}
                                                    remark={selectedResult.channel_key_remark}
                                                    className="max-w-[12rem]"
                                                />
                                            </div>
                                        )}
                                    </div>
                                </div>

                                <div className="min-h-0 flex-1 overflow-auto p-3 sm:p-4">
                                    <div className="flex min-h-full flex-col gap-3">
                                        {selectedAttempts.map((attempt, index) => {
                                            const tone = getAttemptTone(attempt.status);
                                            const responseContent = attempt.response_content?.trim() || attempt.error || '-';

                                            return (
                                                <div
                                                    key={`${selectedTarget.key}-${attempt.channel_key_id ?? attempt.channel_key_index ?? index}-${index}`}
                                                    className={cn('overflow-hidden rounded-2xl border', tone.card)}
                                                >
                                                    <div className={cn('flex items-center gap-2 px-3 py-2 text-xs', tone.divider, 'border-b')}>
                                                        {attempt.status === GroupChannelCheckItemStatus.Success ? (
                                                            <CheckCircle2 className="size-3.5 shrink-0 text-emerald-600" />
                                                        ) : (
                                                            <AlertCircle className="size-3.5 shrink-0 text-destructive" />
                                                        )}
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
                                                    <div className="px-3 py-3">
                                                        <div className="mb-2 text-xs font-medium text-muted-foreground">{responseText}</div>
                                                        <pre className={cn('min-w-0 whitespace-pre-wrap break-all text-xs leading-relaxed', tone.body)}>
                                                            {responseContent}
                                                        </pre>
                                                    </div>
                                                </div>
                                            );
                                        })}

                                        {selectedAttempts.length === 0 && (
                                            <div className="flex min-h-[160px] items-center justify-center rounded-2xl border border-border/70 bg-background/60 text-sm text-muted-foreground">
                                                {withTranslationFallback(t('emptyResult'), '未发送测试', ['group.healthCheck.emptyResult', 'emptyResult'])}
                                            </div>
                                        )}
                                    </div>
                                </div>
                            </div>
                        )}
                    </section>
                </div>
            </DialogContent>
        </Dialog>
    );
}
