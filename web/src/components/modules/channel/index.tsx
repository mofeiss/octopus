'use client';

import { useEffect, useMemo } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { useChannelList } from '@/api/endpoints/channel';
import { Card } from './Card';
import { usePaginationStore, useSearchStore } from '@/components/modules/toolbar';
import { EASING } from '@/lib/animations/fluid-transitions';

export function Channel() {
    const { data: channelsData } = useChannelList();
    const pageKey = 'channel' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const setPage = usePaginationStore((s) => s.setPage);
    const setTotalItems = usePaginationStore((s) => s.setTotalItems);
    const setPageSize = usePaginationStore((s) => s.setPageSize);

    const filteredChannels = useMemo(() => {
        if (!channelsData) return [];
        const sorted = [...channelsData].sort((a, b) => a.raw.id - b.raw.id);
        if (!searchTerm.trim()) return sorted;
        const term = searchTerm.toLowerCase();
        return sorted.filter((c) => c.raw.name.toLowerCase().includes(term));
    }, [channelsData, searchTerm]);

    // Sync to store for Toolbar to display pagination info
    useEffect(() => {
        setTotalItems(pageKey, filteredChannels.length);
        setPageSize(pageKey, Math.max(filteredChannels.length, 1));
    }, [filteredChannels.length, pageKey, setTotalItems, setPageSize]);

    // Reset to page 1 when search term changes
    useEffect(() => {
        setPage(pageKey, 1);
    }, [searchTerm, pageKey, setPage]);

    return (
        <AnimatePresence mode="popLayout" initial={false}>
            <motion.div
                key="channel-list"
                variants={{
                    enter: { opacity: 0, y: 12 },
                    center: { opacity: 1, y: 0 },
                    exit: { opacity: 0, y: -12 },
                }}
                initial="enter"
                animate="center"
                exit="exit"
                transition={{ duration: 0.25, ease: EASING.easeOutExpo }}
            >
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
                    <AnimatePresence mode="popLayout">
                        {filteredChannels.map((channel, index) => (
                            <motion.div
                                key={"channel-" + channel.raw.id}
                                initial={{ opacity: 0, y: 20 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{
                                    opacity: 0,
                                    scale: 0.95,
                                    transition: { duration: 0.2 }
                                }}
                                transition={{
                                    duration: 0.45,
                                    ease: EASING.easeOutExpo,
                                    delay: index === 0 ? 0 : Math.min(0.08 * Math.log2(index + 1), 0.4),
                                }}
                                layout={!searchTerm.trim()}
                            >
                                <Card channel={channel.raw} stats={channel.formatted} />
                            </motion.div>
                        ))}
                    </AnimatePresence>
                </div>
            </motion.div>
        </AnimatePresence>
    );
}
