'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { GroupCard } from './Card';
import { useGroupList } from '@/api/endpoints/group';
import { usePaginationStore, useSearchStore } from '@/components/modules/toolbar';
import { EASING } from '@/lib/animations/fluid-transitions';
import { GroupChannelCheckDialog, type ChannelCheckSelection } from './ChannelCheckDialog';

export function Group() {
    const { data: groups = [] } = useGroupList();
    const pageKey = 'group' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const setPage = usePaginationStore((s) => s.setPage);
    const setTotalItems = usePaginationStore((s) => s.setTotalItems);
    const setPageSize = usePaginationStore((s) => s.setPageSize);
    const [channelCheckOpen, setChannelCheckOpen] = useState(false);
    const [channelCheckInitialSelection, setChannelCheckInitialSelection] = useState<ChannelCheckSelection | null>(null);

    const filteredGroups = useMemo(() => {
        const sorted = [...groups].sort((a, b) => (a.sort_order || a.id!) - (b.sort_order || b.id!)); // [fork] sort by sort_order
        if (!searchTerm.trim()) return sorted;
        const term = searchTerm.toLowerCase();
        return sorted.filter((g) => g.name.toLowerCase().includes(term));
    }, [groups, searchTerm]);

    // Sync to store for Toolbar to display pagination info
    useEffect(() => {
        setTotalItems(pageKey, filteredGroups.length);
        setPageSize(pageKey, Math.max(filteredGroups.length, 1));
    }, [filteredGroups.length, pageKey, setTotalItems, setPageSize]);

    // Reset to page 1 when search term changes
    useEffect(() => {
        setPage(pageKey, 1);
    }, [searchTerm, pageKey, setPage]);

    const handleOpenChannelCheck = useCallback((selection: ChannelCheckSelection) => {
        setChannelCheckInitialSelection(selection);
        setChannelCheckOpen(true);
    }, []);

    return (
        <>
            <AnimatePresence mode="popLayout" initial={false}>
                <motion.div
                    key="group-list"
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
                    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-2 gap-4">
                        <AnimatePresence mode="popLayout">
                            {filteredGroups.map((group, index) => (
                                <motion.div
                                    key={group.id}
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
                                    <GroupCard group={group} onOpenChannelCheck={handleOpenChannelCheck} />
                                </motion.div>
                            ))}
                        </AnimatePresence>
                    </div>
                </motion.div>
            </AnimatePresence>

            <GroupChannelCheckDialog
                open={channelCheckOpen}
                onOpenChange={setChannelCheckOpen}
                groups={groups}
                initialSelection={channelCheckInitialSelection}
            />
        </>
    );
}
