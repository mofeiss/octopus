import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';

export enum GroupChannelCheckTaskStatus {
    Pending = 'pending',
    Running = 'running',
    Success = 'success',
    PartialSuccess = 'partial_success',
    Failed = 'failed',
}

export enum GroupChannelCheckTaskMode {
    Batch = 'batch',
    Single = 'single',
}

export enum GroupChannelCheckItemStatus {
    Pending = 'pending',
    Running = 'running',
    Success = 'success',
    Failed = 'failed',
}

export interface GroupChannelCheckTaskItem {
    id: number;
    task_id: number;
    group_id: number;
    group_item_id?: number;
    channel_id: number;
    channel_name: string;
    channel_type: number;
    model_name: string;
    status: GroupChannelCheckItemStatus;
    request_kind: string;
    request_url: string;
    base_url: string;
    channel_key_id?: number;
    channel_key_index?: number;
    channel_key_preview?: string;
    channel_key_remark?: string;
    response_status_code: number;
    duration_ms: number;
    request_content: string;
    response_preview: string;
    response_content: string;
    error: string;
    started_at: number;
    finished_at: number;
    attempts?: GroupChannelCheckAttempt[];
}

export interface GroupChannelCheckAttempt {
    status: GroupChannelCheckItemStatus;
    channel_key_id?: number;
    channel_key_index?: number;
    channel_key_preview?: string;
    channel_key_remark?: string;
    response_status_code: number;
    response_content: string;
    error: string;
}

export interface GroupChannelCheckTask {
    id: number;
    group_id: number;
    group_name: string;
    mode: GroupChannelCheckTaskMode;
    status: GroupChannelCheckTaskStatus;
    total_count: number;
    pending_count: number;
    running_count: number;
    success_count: number;
    failed_count: number;
    created_at: number;
    started_at: number;
    finished_at: number;
    last_error: string;
    items?: GroupChannelCheckTaskItem[];
}

export interface GroupChannelCheckSyncResult {
    group_id: number;
    group_name: string;
    enabled_count: number;
    disabled_count: number;
    skipped_count: number;
}

export interface GroupChannelCheckCreateRequest {
    group_id: number;
    group_item_id?: number;
    channel_id?: number;
    model_name?: string;
}

export const groupChannelCheckLatestQueryKey = ['group-channel-check', 'latest'] as const;
export const groupChannelCheckDetailQueryKey = (taskId: number) => ['group-channel-check', 'detail', taskId] as const;

export function isGroupChannelCheckTaskActive(task?: Pick<GroupChannelCheckTask, 'status'> | null) {
    return task?.status === GroupChannelCheckTaskStatus.Pending || task?.status === GroupChannelCheckTaskStatus.Running;
}

export function useCreateGroupChannelCheckTask() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: GroupChannelCheckCreateRequest) => {
            return apiClient.post<GroupChannelCheckTask>('/api/v1/group/channel-check/create', data);
        },
        onSuccess: (task) => {
            logger.log('渠道测活任务创建成功:', task);
            queryClient.setQueryData(groupChannelCheckDetailQueryKey(task.id), task);
            queryClient.invalidateQueries({ queryKey: groupChannelCheckLatestQueryKey });
        },
        onError: (error) => {
            logger.error('渠道测活任务创建失败:', error);
        },
    });
}

export function useGroupChannelCheckLatestTasks() {
    return useQuery({
        queryKey: groupChannelCheckLatestQueryKey,
        queryFn: async () => {
            return apiClient.get<GroupChannelCheckTask[]>('/api/v1/group/channel-check/latest');
        },
        refetchInterval: 5000,
    });
}

export function useGroupChannelCheckTaskDetail(taskId?: number, enabled = true) {
    return useQuery({
        queryKey: taskId ? groupChannelCheckDetailQueryKey(taskId) : ['group-channel-check', 'detail', 'idle'],
        queryFn: async () => {
            return apiClient.get<GroupChannelCheckTask>(`/api/v1/group/channel-check/detail/${taskId}`);
        },
        enabled: enabled && !!taskId,
        refetchInterval: (query) => {
            const task = query.state.data as GroupChannelCheckTask | undefined;
            return isGroupChannelCheckTaskActive(task) ? 2000 : false;
        },
    });
}

export function useSyncGroupChannelCheckTaskStatus() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (taskId: number) => {
            return apiClient.post<GroupChannelCheckSyncResult>(`/api/v1/group/channel-check/sync-status/${taskId}`, {});
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
            queryClient.invalidateQueries({ queryKey: groupChannelCheckLatestQueryKey });
        },
    });
}
