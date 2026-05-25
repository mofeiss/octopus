'use client';

import { useState, useMemo, useCallback, useEffect, useRef } from 'react';
import { Trash2, X, Pencil, Activity } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { type Group, useDeleteGroup, useUpdateGroup, useEnableGroupItem } from '@/api/endpoints/group';
import { useModelChannelList } from '@/api/endpoints/model';
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';
import { toast } from '@/components/common/Toast';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import type { SelectedMember } from './ItemList';
import { MemberList } from './ItemList';
import { GroupEditor, type GroupEditorValues } from './Editor';
import { buildChannelNameByModelKey, modelChannelKey, MODE_LABELS } from './utils';
import { GroupMode, type GroupUpdateRequest } from '@/api/endpoints/group';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import {
    MorphingDialog,
    MorphingDialogClose,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogDescription,
    MorphingDialogTitle,
    MorphingDialogTrigger,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import type { ChannelCheckSelection } from './ChannelCheckDialog';

const GROUP_MODE_OPTIONS = [
    GroupMode.Failover,
    GroupMode.RoundRobin,
    GroupMode.Random,
    GroupMode.Weighted,
] as const;

function withTranslationFallback(translated: string, fallback: string, keys: string[]) {
    const normalized = translated.trim();
    if (!normalized) return fallback;
    if (normalized.startsWith('MISSING_MESSAGE') || normalized.startsWith('MISSING_TRANSLATION')) return fallback;
    if (keys.includes(normalized)) return fallback;
    if (keys.some((key) => normalized.endsWith(key))) return fallback;
    return translated;
}

interface EditDialogContentProps {
    group: Group;
    displayMembers: SelectedMember[];
    isSubmitting: boolean;
    onSubmit: (values: GroupEditorValues, onDone?: () => void) => void;
}

function EditDialogContent({ group, displayMembers, isSubmitting, onSubmit }: EditDialogContentProps) {
    const { setIsOpen } = useMorphingDialog();
    const t = useTranslations('group');
    return (
        <>
            <MorphingDialogTitle className="shrink-0">
                <header className="mb-3 flex items-center justify-between">
                    <h2 className="text-2xl font-bold text-card-foreground">
                        {t('detail.actions.edit')}
                    </h2>
                    <MorphingDialogClose className="relative right-0 top-0" />
                </header>
            </MorphingDialogTitle>
            <MorphingDialogDescription className="flex-1 min-h-0 overflow-hidden">
                <GroupEditor
                    key={`edit-group-${group.id}`}
                    initial={{
                        name: group.name,
                        match_regex: group.match_regex ?? '',
                        mode: group.mode,
                        first_token_time_out: group.first_token_time_out ?? 0,
                        session_keep_time: group.session_keep_time ?? 0,
                        route_aliases: group.route_aliases ?? '', // [fork]
                        remark: group.remark ?? '', // [fork]
                        members: displayMembers,
                    }}
                    submitText={t('detail.actions.save')}
                    submittingText={t('create.submitting')}
                    isSubmitting={isSubmitting}
                    onCancel={() => setIsOpen(false)}
                    onSubmit={(v) => onSubmit(v, () => setIsOpen(false))}
                />
            </MorphingDialogDescription>
        </>
    );
}

export function GroupCard({ group, onOpenChannelCheck }: { group: Group; onOpenChannelCheck: (selection: ChannelCheckSelection) => void }) {
    const t = useTranslations('group');
    const updateGroup = useUpdateGroup();
    const deleteGroup = useDeleteGroup();
    const enableGroupItem = useEnableGroupItem(); // [fork]
    const { data: modelChannels = [] } = useModelChannelList();

    const [confirmDelete, setConfirmDelete] = useState(false);
    const [members, setMembers] = useState<SelectedMember[]>([]);
    const isDragging = useRef(false);
    const weightTimerRef = useRef<NodeJS.Timeout | null>(null);
    const membersRef = useRef<SelectedMember[]>([]);

    const channelNameByKey = useMemo(() => buildChannelNameByModelKey(modelChannels), [modelChannels]);
    const enabledByKey = useMemo(() => {
        const map = new Map<string, boolean>();
        modelChannels.forEach((mc) => {
            map.set(modelChannelKey(mc.channel_id, mc.name), mc.enabled);
        });
        return map;
    }, [modelChannels]);

    const displayMembers = useMemo((): SelectedMember[] =>
        [...(group.items || [])]
            .sort((a, b) => a.priority - b.priority)
            .map((item) => ({
                id: modelChannelKey(item.channel_id, item.model_name),
                name: item.model_name,
                enabled: (enabledByKey.get(modelChannelKey(item.channel_id, item.model_name)) ?? true) && (item.enabled !== false), // [fork] 组合渠道级 + item级
                item_enabled: item.enabled !== false, // [fork] item 级原始状态
                channel_id: item.channel_id,
                channel_name: channelNameByKey.get(modelChannelKey(item.channel_id, item.model_name)) ?? `Channel ${item.channel_id}`,
                item_id: item.id,
                weight: item.weight,
                health_check_task_id: item.health_check_task_id,
                health_check_status: item.health_check_status,
                health_check_checked_at: item.health_check_checked_at,
                health_check_consecutive_failures: item.health_check_consecutive_failures,
                health_check_response_status_code: item.health_check_response_status_code,
                health_check_duration_ms: item.health_check_duration_ms,
                health_check_error: item.health_check_error,
            })),
        [group.items, channelNameByKey, enabledByKey]
    );

    useEffect(() => {
        if (!isDragging.current) {
            // Keep current drag/edit UX behavior; this sync is intentionally effect-driven.
            // eslint-disable-next-line react-hooks/set-state-in-effect
            setMembers([...displayMembers]);
        }
    }, [displayMembers]);

    useEffect(() => {
        membersRef.current = members;
    }, [members]);

    useEffect(() => {
        return () => { if (weightTimerRef.current) clearTimeout(weightTimerRef.current); };
    }, []);

    const onSuccess = useCallback(() => toast.success(t('toast.updated')), [t]);
    const onError = useCallback((error: Error) => toast.error(t('toast.updateFailed'), { description: error.message }), [t]);

    // Avoid UI flicker: drag-reorder also uses the same mutation, so only "mode switch" should lock mode buttons.
    const isUpdatingMode = (() => {
        if (!updateGroup.isPending) return false;
        const v = updateGroup.variables;
        if (typeof v !== 'object' || v === null) return false;
        return 'mode' in v && typeof (v as { mode?: unknown }).mode === 'number';
    })();
    const handleModeSelect = useCallback((value: string) => {
        if (isUpdatingMode || !group.id) return;
        const nextMode = Number(value) as GroupMode;
        if (nextMode === group.mode) return;
        updateGroup.mutate({ id: group.id, mode: nextMode }, { onSuccess, onError });
    }, [group.id, group.mode, isUpdatingMode, onError, onSuccess, updateGroup]);

    const priorityByItemId = useMemo(() => {
        const map = new Map<number, number>();
        (group.items || []).forEach((item) => {
            if (item.id !== undefined) map.set(item.id, item.priority);
        });
        return map;
    }, [group.items]);

    const handleDragStart = useCallback(() => { isDragging.current = true; }, []);
    const handleDragFinish = useCallback(() => { isDragging.current = false; }, []);

    const handleDropReorder = useCallback((nextMembers: SelectedMember[]) => {
        const itemsToUpdate = nextMembers
            .map((m, i) => ({ member: m, newPriority: i + 1 }))
            .filter(({ member, newPriority }) => {
                if (!member.item_id) return false;
                const origPriority = priorityByItemId.get(member.item_id);
                return origPriority !== undefined && origPriority !== newPriority;
            })
            .map(({ member, newPriority }) => ({ id: member.item_id!, priority: newPriority, weight: member.weight ?? 1 }));
        if (itemsToUpdate.length > 0) updateGroup.mutate({ id: group.id!, items_to_update: itemsToUpdate }, { onSuccess, onError });
    }, [group.id, priorityByItemId, updateGroup, onSuccess, onError]);

    const handleRemoveMember = useCallback((id: string) => {
        const member = members.find((m) => m.id === id);
        if (member?.item_id !== undefined) updateGroup.mutate({ id: group.id!, items_to_delete: [member.item_id] }, { onSuccess, onError });
    }, [members, group.id, updateGroup, onSuccess, onError]);

    const handleWeightChange = useCallback((id: string, weight: number) => {
        setMembers((prev) => prev.map((m) => m.id === id ? { ...m, weight } : m));
        if (weightTimerRef.current) clearTimeout(weightTimerRef.current);
        weightTimerRef.current = setTimeout(() => {
            const member = membersRef.current.find((m) => m.id === id);
            if (!member?.item_id) return;
            const priority = priorityByItemId.get(member.item_id);
            if (!priority) return;
            updateGroup.mutate(
                { id: group.id!, items_to_update: [{ id: member.item_id, priority, weight }] },
                { onSuccess, onError }
            );
        }, 500);
    }, [group.id, priorityByItemId, updateGroup, onSuccess, onError]);

    // [fork] item 级启用/禁用
    const handleToggleItemEnabled = useCallback((id: string, enabled: boolean) => {
        const member = membersRef.current.find((m) => m.id === id);
        if (member?.item_id === undefined) return;
        enableGroupItem.mutate(
            { id: member.item_id, enabled },
            {
                onSuccess: () => toast.success(t(enabled ? 'toast.itemEnabled' : 'toast.itemDisabled')),
                onError,
            }
        );
    }, [enableGroupItem, t, onError]);

    const handleOpenGroupChannelCheck = useCallback(() => {
        if (!group.id) return;
        onOpenChannelCheck({ groupId: group.id });
    }, [group.id, onOpenChannelCheck]);

    const handleCreateSingleChannelCheck = useCallback((member: SelectedMember) => {
        if (!group.id) return;
        onOpenChannelCheck({
            groupId: group.id,
            groupItemId: member.item_id,
            channelId: member.channel_id,
            modelName: member.name,
        });
    }, [group.id, onOpenChannelCheck]);

    const handleSubmitEdit = useCallback((values: GroupEditorValues, onDone?: () => void) => {
        if (!group.id) return;

        const originalItems = [...(group.items || [])].sort((a, b) => a.priority - b.priority);
        const originalById = new Map<number, { priority: number; weight: number }>();
        const originalIds = new Set<number>();
        originalItems.forEach((it) => {
            if (typeof it.id === 'number') {
                originalIds.add(it.id);
                originalById.set(it.id, { priority: it.priority, weight: it.weight });
            }
        });

        const newIds = new Set<number>();
        values.members.forEach((m) => { if (typeof m.item_id === 'number') newIds.add(m.item_id); });

        const items_to_delete = Array.from(originalIds).filter((id) => !newIds.has(id));

        const items_to_add = values.members
            .map((m, idx) => ({ m, priority: idx + 1 }))
            .filter(({ m }) => typeof m.item_id !== 'number')
            .map(({ m, priority }) => ({
                channel_id: m.channel_id,
                model_name: m.name,
                priority,
                weight: m.weight ?? 1,
            }));

        const items_to_update = values.members
            .map((m, idx) => ({ m, priority: idx + 1 }))
            .filter(({ m }) => typeof m.item_id === 'number')
            .map(({ m, priority }) => {
                const id = m.item_id!;
                const orig = originalById.get(id);
                const weight = m.weight ?? 1;
                if (!orig) return null;
                if (orig.priority === priority && orig.weight === weight) return null;
                return { id, priority, weight };
            })
            .filter((x): x is { id: number; priority: number; weight: number } => x !== null);

        const payload: GroupUpdateRequest = { id: group.id };
        const nextName = values.name.trim();
        const nextRegex = (values.match_regex ?? '').trim();
        const nextFirstTokenTimeOut = values.first_token_time_out ?? 0;
        const nextSessionKeepTime = values.session_keep_time ?? 0;

        if (nextName && nextName !== group.name) payload.name = nextName;
        if (values.mode !== group.mode) payload.mode = values.mode;
        if (nextRegex !== (group.match_regex ?? '')) payload.match_regex = nextRegex;
        if (nextFirstTokenTimeOut !== (group.first_token_time_out ?? 0)) payload.first_token_time_out = nextFirstTokenTimeOut;
        if (nextSessionKeepTime !== (group.session_keep_time ?? 0)) payload.session_keep_time = nextSessionKeepTime;
        const nextRouteAliases = (values.route_aliases ?? '').trim(); // [fork]
        if (nextRouteAliases !== (group.route_aliases ?? '')) payload.route_aliases = nextRouteAliases; // [fork]
        if (values.remark !== (group.remark ?? '')) payload.remark = values.remark; // [fork]
        if (items_to_add.length) payload.items_to_add = items_to_add;
        if (items_to_update.length) payload.items_to_update = items_to_update;
        if (items_to_delete.length) payload.items_to_delete = items_to_delete;

        if (Object.keys(payload).length === 1) {
            onDone?.();
            return;
        }

        updateGroup.mutate(payload, {
            onSuccess: () => {
                onSuccess();
                onDone?.();
            },
            onError,
        });
    }, [group.first_token_time_out, group.session_keep_time, group.route_aliases, group.remark, group.id, group.items, group.match_regex, group.mode, group.name, onSuccess, onError, updateGroup]);

    return (
        <>
            <article className="flex flex-col h-full rounded-3xl border border-border bg-card text-card-foreground p-4 custom-shadow">
                <header className="flex items-start justify-between mb-3 relative overflow-visible rounded-xl -mx-1 px-1 -my-1 py-1">
                    <div className="relative mr-2 flex min-w-0 flex-1 items-center gap-2 group/title">
                        <Tooltip side="top" sideOffset={10} align="center">
                            <TooltipTrigger asChild>
                                <h3 className={cn('truncate text-lg font-bold', group.remark ? 'max-w-[45%] sm:max-w-[50%]' : 'max-w-full')}>
                                    {group.name}
                                </h3>
                            </TooltipTrigger>
                            <TooltipContent key={group.name}>{group.name}</TooltipContent>
                        </Tooltip>
                        {group.remark && (
                            <>
                                <span className="shrink-0 text-muted-foreground/40">/</span>
                                <Tooltip side="top" sideOffset={10} align="start">
                                    <TooltipTrigger asChild>
                                        <span className="min-w-0 flex-1 truncate text-sm text-primary">
                                            {group.remark}
                                        </span>
                                    </TooltipTrigger>
                                    <TooltipContent key={`remark-${group.id ?? group.name}`}>{group.remark}</TooltipContent>
                                </Tooltip>
                            </>
                        )}
                    </div>

                    <div className="flex items-center gap-1 shrink-0">
                        <MorphingDialog>
                            <MorphingDialogTrigger className="p-1.5 rounded-lg transition-colors hover:bg-muted text-muted-foreground hover:text-foreground">
                                <Tooltip side="top" sideOffset={10} align="center">
                                    <TooltipTrigger asChild>
                                        <Pencil className="size-4" />
                                    </TooltipTrigger>
                                    <TooltipContent>{t('detail.actions.edit')}</TooltipContent>
                                </Tooltip>
                            </MorphingDialogTrigger>

                            <MorphingDialogContainer>
                                <MorphingDialogContent className="relative w-screen max-w-full md:max-w-4xl bg-card text-card-foreground px-6 py-4 rounded-3xl custom-shadow h-[calc(100vh-2rem)] flex flex-col overflow-hidden">
                                    <EditDialogContent
                                        group={group}
                                        displayMembers={displayMembers}
                                        isSubmitting={updateGroup.isPending}
                                        onSubmit={handleSubmitEdit}
                                    />
                                </MorphingDialogContent>
                            </MorphingDialogContainer>
                        </MorphingDialog>

                        {!confirmDelete && (
                            <Tooltip side="top" sideOffset={10} align="center">
                                <TooltipTrigger>
                                    <motion.button layoutId={`delete-btn-group-${group.id}`} type="button" onClick={() => setConfirmDelete(true)} className="p-1.5 rounded-lg hover:bg-destructive/10 text-muted-foreground hover:text-destructive transition-colors">
                                        <Trash2 className="size-4" />
                                    </motion.button>
                                </TooltipTrigger>
                                <TooltipContent>{t('detail.actions.delete')}</TooltipContent>
                            </Tooltip>
                        )}
                    </div>

                    <AnimatePresence>
                        {confirmDelete && (
                            <motion.div layoutId={`delete-btn-group-${group.id}`} className="absolute inset-0 flex items-center justify-center gap-2 bg-destructive p-2 rounded-xl" transition={{ type: 'spring', stiffness: 400, damping: 30 }}>
                                <button type="button" onClick={() => setConfirmDelete(false)} className="flex h-7 w-7 items-center justify-center rounded-lg bg-destructive-foreground/20 text-destructive-foreground transition-all hover:bg-destructive-foreground/30 active:scale-95">
                                    <X className="size-4" />
                                </button>
                                <button type="button" onClick={() => group.id && deleteGroup.mutate(group.id, { onSuccess: () => toast.success(t('toast.deleted')) })} disabled={deleteGroup.isPending} className="flex-1 h-7 flex items-center justify-center gap-2 rounded-lg bg-destructive-foreground text-destructive text-sm font-semibold transition-all hover:bg-destructive-foreground/90 active:scale-[0.98] disabled:opacity-50 disabled:cursor-not-allowed">
                                    <Trash2 className="size-3.5" />
                                    {t('detail.actions.confirmDelete')}
                                </button>
                            </motion.div>
                        )}
                    </AnimatePresence>
                </header>

                <div className="mb-3 flex flex-nowrap items-center gap-1.5 overflow-x-auto pb-1 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
                    <Button
                        type="button"
                        variant="secondary"
                        onClick={handleOpenGroupChannelCheck}
                        disabled={!group.id}
                        className="h-8 shrink-0 rounded-xl px-2.5 text-xs"
                    >
                        <Activity className="size-3.5" />
                        {withTranslationFallback(
                            t('healthCheck.actions.open'),
                            '渠道测活',
                            ['group.healthCheck.actions.open', 'healthCheck.actions.open']
                        )}
                    </Button>

                    <Select
                        value={String(group.mode)}
                        onValueChange={handleModeSelect}
                        disabled={isUpdatingMode || !group.id}
                    >
                        <SelectTrigger className="h-8 w-[7.75rem] shrink-0 rounded-xl px-2.5 text-xs">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {GROUP_MODE_OPTIONS.map((mode) => (
                                <SelectItem key={mode} value={String(mode)}>
                                    {t(`mode.${MODE_LABELS[mode]}`)}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </div>

                <section className="rounded-xl border border-border/50 bg-muted/30 overflow-hidden relative flex-1 min-h-20">
                    <MemberList
                        members={members}
                        onReorder={setMembers}
                        onRemove={handleRemoveMember}
                        onWeightChange={handleWeightChange}
                        onToggleEnabled={handleToggleItemEnabled}
                        onProbe={handleCreateSingleChannelCheck}
                        onDragStart={handleDragStart}
                        onDrop={handleDropReorder}
                        onDragFinish={handleDragFinish}
                        autoScrollOnAdd={false}
                        showWeight={group.mode === GroupMode.Weighted}
                        layoutScope={`card-${group.id ?? 'unknown'}`}
                    />
                </section>
            </article>
        </>
    );
}
