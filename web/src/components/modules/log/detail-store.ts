import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type LogRequestSectionKey = 'original' | 'outbound';
export type LogResponseSectionKey = 'original' | 'outbound';

interface LogDetailState {
    activeRequestSection: LogRequestSectionKey | null;
    activeResponseSection: LogResponseSectionKey | null;
    setActiveRequestSection: (section: LogRequestSectionKey | null) => void;
    setActiveResponseSection: (section: LogResponseSectionKey | null) => void;
}

export const useLogDetailStore = create<LogDetailState>()(
    persist(
        (set) => ({
            activeRequestSection: 'original',
            activeResponseSection: 'original',
            setActiveRequestSection: (section) => set({ activeRequestSection: section }),
            setActiveResponseSection: (section) => set({ activeResponseSection: section }),
        }),
        {
            name: 'octopus-log-detail',
        }
    )
);
