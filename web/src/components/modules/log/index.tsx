'use client';

import { useQueryClient } from '@tanstack/react-query';
import { type ReactNode, useCallback, useEffect, useMemo, useRef } from 'react';
import { type LogScope, type RelayLog, logDetailQueryKey, prefetchLogDetail, useLogs } from '@/api/endpoints/log';
import { PageWrapper } from '@/components/common/PageWrapper';
import { LogCard } from './Item';
import { Loader2 } from 'lucide-react';
import { useLocale, useTranslations } from 'next-intl';
import { logger } from '@/lib/logger';

const LOG_SEGMENT_GAP_SECONDS = 3 * 60;
const DETAIL_PREFETCH_LIMIT = 5;
const DETAIL_PREFETCH_CONCURRENCY = 5;

function normalizeLocale(locale: string): string {
    if (locale === 'zh_hans') return 'zh-CN';
    if (locale === 'zh_hant') return 'zh-TW';
    return 'en-US';
}

function formatSegmentTime(timestamp: number, locale: string): string {
    const date = new Date(timestamp * 1000);
    return date.toLocaleString(normalizeLocale(locale), {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
    });
}

/**
 * 日志页面组件
 * - 初始加载20条历史日志
 * - SSE 实时推送新日志
 * - 滚动自动加载更多
 */
export function Log({ scope = 'admin' }: { scope?: LogScope }) {
    const t = useTranslations('log');
    const locale = useLocale();
    const queryClient = useQueryClient();
    const { logs, hasMore, isLoading, isLoadingMore, loadMore } = useLogs({ pageSize: 10, scope });
    const loadMoreRef = useRef<HTMLDivElement>(null);
    const armedRef = useRef(true);
    const initialPrefetchDoneRef = useRef(false);
    const prefetchedIdsRef = useRef<Set<number>>(new Set());
    const queuedIdsRef = useRef<Set<number>>(new Set());
    const inflightIdsRef = useRef<Set<number>>(new Set());
    const prefetchQueueRef = useRef<number[]>([]);
    const pendingNeighborSeedRef = useRef<number | null>(null);
    const pumpPrefetchQueueRef = useRef<() => void>(() => { });

    const hasDetailCache = useCallback((id: number) => {
        return !!queryClient.getQueryData(logDetailQueryKey(scope, id));
    }, [queryClient, scope]);

    const pumpPrefetchQueue = useCallback(() => {
        while (
            inflightIdsRef.current.size < DETAIL_PREFETCH_CONCURRENCY &&
            prefetchQueueRef.current.length > 0
        ) {
            const id = prefetchQueueRef.current.shift();
            if (typeof id !== 'number') break;

            queuedIdsRef.current.delete(id);
            if (prefetchedIdsRef.current.has(id)) continue;
            if (hasDetailCache(id)) {
                prefetchedIdsRef.current.add(id);
                continue;
            }

            inflightIdsRef.current.add(id);
            void prefetchLogDetail(queryClient, scope, id)
                .then(() => {
                    prefetchedIdsRef.current.add(id);
                })
                .catch((e) => {
                    logger.warn('日志详情预加载失败:', e);
                })
                .finally(() => {
                    inflightIdsRef.current.delete(id);
                    pumpPrefetchQueueRef.current();
                });
        }
    }, [hasDetailCache, queryClient, scope]);

    useEffect(() => {
        pumpPrefetchQueueRef.current = pumpPrefetchQueue;
    }, [pumpPrefetchQueue]);

    const enqueueLogIdForPrefetch = useCallback((id: number) => {
        if (id <= 0) return;
        if (prefetchedIdsRef.current.has(id)) return;
        if (queuedIdsRef.current.has(id)) return;
        if (inflightIdsRef.current.has(id)) return;
        if (hasDetailCache(id)) {
            prefetchedIdsRef.current.add(id);
            return;
        }

        queuedIdsRef.current.add(id);
        prefetchQueueRef.current.push(id);
        pumpPrefetchQueue();
    }, [hasDetailCache, pumpPrefetchQueue]);

    const enqueueLogForPrefetch = useCallback((log: RelayLog | undefined) => {
        if (!log) return;
        if (log.content_omitted !== true) {
            prefetchedIdsRef.current.add(log.id);
            return;
        }
        enqueueLogIdForPrefetch(log.id);
    }, [enqueueLogIdForPrefetch]);

    const prefetchNeighbors = useCallback((seedLogID: number, allowLoadMore: boolean) => {
        const index = logs.findIndex((log) => log.id === seedLogID);
        if (index < 0) return;

        enqueueLogForPrefetch(logs[index]);
        enqueueLogForPrefetch(logs[index - 1]); // newer neighbor

        const olderNeighbor = logs[index + 1];
        if (olderNeighbor) {
            enqueueLogForPrefetch(olderNeighbor);
            return;
        }

        if (!allowLoadMore) return;
        if (pendingNeighborSeedRef.current !== null) return;
        if (!hasMore || isLoadingMore) return;

        pendingNeighborSeedRef.current = seedLogID;
        void loadMore().catch((e) => {
            pendingNeighborSeedRef.current = null;
            logger.warn('日志详情相邻补页失败:', e);
        });
    }, [enqueueLogForPrefetch, hasMore, isLoadingMore, loadMore, logs]);

    const handleLogOpen = useCallback((logID: number) => {
        prefetchNeighbors(logID, true);
    }, [prefetchNeighbors]);
    const segmentedLogs = useMemo(() => {
        return logs.map((log, index) => {
            if (index === 0) return { log, showDivider: false };

            const previous = logs[index - 1];
            const showDivider = previous.time - log.time >= LOG_SEGMENT_GAP_SECONDS;
            return { log, showDivider };
        });
    }, [logs]);

    useEffect(() => {
        initialPrefetchDoneRef.current = false;
        prefetchedIdsRef.current.clear();
        queuedIdsRef.current.clear();
        inflightIdsRef.current.clear();
        prefetchQueueRef.current = [];
        pendingNeighborSeedRef.current = null;
    }, [scope]);

    useEffect(() => {
        for (const log of logs) {
            if (log.content_omitted !== true) {
                prefetchedIdsRef.current.add(log.id);
            } else if (hasDetailCache(log.id)) {
                prefetchedIdsRef.current.add(log.id);
            }
        }
    }, [hasDetailCache, logs]);

    useEffect(() => {
        if (initialPrefetchDoneRef.current) return;
        if (logs.length === 0) return;

        initialPrefetchDoneRef.current = true;
        logs.slice(0, DETAIL_PREFETCH_LIMIT).forEach((log) => {
            enqueueLogForPrefetch(log);
        });
    }, [enqueueLogForPrefetch, logs]);

    useEffect(() => {
        const pendingSeedLogID = pendingNeighborSeedRef.current;
        if (pendingSeedLogID === null) return;

        prefetchNeighbors(pendingSeedLogID, false);
        pendingNeighborSeedRef.current = null;
    }, [logs, prefetchNeighbors]);

    useEffect(() => {
        const target = loadMoreRef.current;
        if (!target) return;

        const observer = new IntersectionObserver(
            (entries) => {
                const entry = entries[0];
                if (!entry) return;

                if (!entry.isIntersecting) {
                    armedRef.current = true;
                    return;
                }

                if (!armedRef.current) return;
                if (!hasMore || isLoading || isLoadingMore || logs.length === 0) return;

                armedRef.current = false;
                loadMore();
            },
            { rootMargin: '100px' }
        );

        observer.observe(target);
        return () => observer.disconnect();
    }, [hasMore, isLoading, isLoadingMore, loadMore, logs.length]);

    const renderedItems = useMemo(() => {
        return segmentedLogs.flatMap(({ log, showDivider }) => {
            const items: ReactNode[] = [];
            if (showDivider) {
                const time = formatSegmentTime(log.time, locale);
                const label = t('list.timeGap', { time });
                const safeLabel = (label === 'log.list.timeGap' || label === 'list.timeGap')
                    ? (locale === 'zh_hant' ? `較早日誌 · ${time}` : locale === 'en' ? `Older logs: ${time}` : `更早日志 · ${time}`)
                    : label;

                items.push(
                    <div key={`divider-${log.id}`} className="flex h-7 items-center gap-3 px-1">
                        <div className="h-px flex-1 bg-border/80" />
                        <span className="text-[11px] leading-none tracking-wide text-muted-foreground bg-muted/60 border border-border/60 rounded-full px-3 py-1.5">
                            {safeLabel}
                        </span>
                        <div className="h-px flex-1 bg-border/80" />
                    </div>
                );
            }

            items.push(<LogCard key={`log-${log.id}`} log={log} scope={scope} onOpenLog={handleLogOpen} />);
            return items;
        });
    }, [handleLogOpen, locale, scope, segmentedLogs, t]);

    return (
        <PageWrapper className="grid grid-cols-1 gap-4">
            {renderedItems}

            <div ref={loadMoreRef} className="flex justify-center py-4">
                {hasMore && (isLoadingMore || isLoading) && (
                    <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                )}
                {!hasMore && logs.length > 0 && (
                    <span className="text-sm text-muted-foreground">{t('list.noMore')}</span>
                )}
            </div>
        </PageWrapper>
    );
}
