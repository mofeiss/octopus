import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type LogRequestSectionKey = 'original' | 'outbound';
export type LogResponseSectionKey = 'original' | 'preview' | 'outbound';
// [fork] Log detail visualization tab/source state.
export type LogDetailTabKey = 'json_sse' | 'visualization';
export type LogVisualSourceMode = 'raw' | 'converted';
export type LogVisualContentMode = 'source' | 'preview';
export type LogVisualItemClickMode = 'expand' | 'maximize';

interface LogDetailState {
    activeRequestSection: LogRequestSectionKey | null;
    activeResponseSection: LogResponseSectionKey | null;
    activeDetailTab: LogDetailTabKey;
    visualSourceMode: LogVisualSourceMode;
    activeVisualItemId: string | null;
    visualContentMode: LogVisualContentMode;
    visualItemClickMode: LogVisualItemClickMode;
    setActiveRequestSection: (section: LogRequestSectionKey | null) => void;
    setActiveResponseSection: (section: LogResponseSectionKey | null) => void;
    setActiveDetailTab: (tab: LogDetailTabKey) => void;
    setVisualSourceMode: (mode: LogVisualSourceMode) => void;
    setActiveVisualItemId: (itemId: string | null) => void;
    setVisualContentMode: (mode: LogVisualContentMode) => void;
    setVisualItemClickMode: (mode: LogVisualItemClickMode) => void;
}

export const useLogDetailStore = create<LogDetailState>()(
    persist(
        (set) => ({
            activeRequestSection: 'original',
            activeResponseSection: 'original',
            activeDetailTab: 'json_sse',
            visualSourceMode: 'raw',
            activeVisualItemId: null,
            visualContentMode: 'source',
            visualItemClickMode: 'maximize',
            setActiveRequestSection: (section) => set({ activeRequestSection: section }),
            setActiveResponseSection: (section) => set({ activeResponseSection: section }),
            setActiveDetailTab: (tab) => set({ activeDetailTab: tab }),
            setVisualSourceMode: (mode) => set({ visualSourceMode: mode }),
            setActiveVisualItemId: (itemId) => set({ activeVisualItemId: itemId }),
            setVisualContentMode: (mode) => set({ visualContentMode: mode }),
            setVisualItemClickMode: (mode) => set({ visualItemClickMode: mode }),
        }),
        {
            name: 'octopus-log-detail',
        }
    )
);
