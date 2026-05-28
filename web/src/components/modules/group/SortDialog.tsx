'use client';

// [fork] 分组排序弹窗组件
import { useState, useCallback } from 'react';
import { GripVertical } from 'lucide-react';
import {
    DragDropContext,
    Draggable,
    Droppable,
    type DraggableProvided,
    type DropResult,
} from '@hello-pangea/dnd';
import { cn } from '@/lib/utils';
import { type Group, useGroupList, useReorderGroups } from '@/api/endpoints/group';
import { toast } from '@/components/common/Toast';
import { useTranslations } from 'next-intl';
import {
    MorphingDialogClose,
    MorphingDialogDescription,
    MorphingDialogTitle,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { Button } from '@/components/ui/button';

function reorderList<T>(list: T[], startIndex: number, endIndex: number): T[] {
    const result = [...list];
    const [removed] = result.splice(startIndex, 1);
    result.splice(endIndex, 0, removed);
    return result;
}

function SortGroupItem({
    group,
    index,
    dnd,
}: {
    group: Group;
    index: number;
    dnd: {
        innerRef: DraggableProvided['innerRef'];
        draggableProps: DraggableProvided['draggableProps'];
        dragHandleProps: DraggableProvided['dragHandleProps'];
        isDragging: boolean;
    };
}) {
    return (
        <div
            // [fork] DnD libraries provide imperative refs/props; this is required for drag behavior.
            // eslint-disable-next-line react-hooks/refs
            ref={dnd.innerRef}
            // eslint-disable-next-line react-hooks/refs
            {...dnd.draggableProps}
            className="rounded-lg"
            // eslint-disable-next-line react-hooks/refs
            style={{
                /* eslint-disable-next-line react-hooks/refs */
                ...(dnd.draggableProps?.style ?? {}),
                /* eslint-disable-next-line react-hooks/refs */
                ...(dnd.isDragging ? { zIndex: 50, boxShadow: '0 8px 32px rgba(0,0,0,0.15)' } : null),
            }}
        >
            <div className={cn(
                'flex items-center gap-2 rounded-lg bg-background border border-border/50 px-2.5 py-2 select-none',
            )}>
                <span className="size-5 rounded-md text-xs font-bold grid place-items-center shrink-0 bg-primary/10 text-primary">
                    {index + 1}
                </span>

                <div
                    className="p-0.5 rounded touch-none cursor-grab active:cursor-grabbing hover:bg-muted"
                    // eslint-disable-next-line react-hooks/refs
                    {...dnd.dragHandleProps}
                >
                    <GripVertical className="size-3.5 text-muted-foreground" />
                </div>

                <span className="text-sm font-medium truncate">{group.name}</span>
            </div>
        </div>
    );
}

export function SortDialogContent() {
    const { setIsOpen } = useMorphingDialog();
    const { data: groups } = useGroupList();
    const reorderGroups = useReorderGroups();
    const t = useTranslations('group');

    const [sortedGroups, setSortedGroups] = useState<Group[]>(() => {
        if (!groups) return [];
        return [...groups].sort((a, b) => (a.sort_order || a.id!) - (b.sort_order || b.id!));
    });

    const handleDragEnd = useCallback((result: DropResult) => {
        const { destination, source } = result;
        if (!destination) return;
        if (destination.index === source.index) return;
        setSortedGroups((prev) => reorderList(prev, source.index, destination.index));
    }, []);

    const handleSave = useCallback(() => {
        const orders = sortedGroups.map((g, i) => ({
            id: g.id!,
            sort_order: i + 1,
        }));
        reorderGroups.mutate({ orders }, {
            onSuccess: () => {
                toast.success(t('sort.saved'));
                setIsOpen(false);
            },
            onError: () => {
                toast.error(t('sort.saveFailed'));
            },
        });
    }, [sortedGroups, reorderGroups, t, setIsOpen]);

    const handleCancel = useCallback(() => {
        setIsOpen(false);
    }, [setIsOpen]);

    return (
        <>
            <MorphingDialogTitle className="shrink-0">
                <header className="mb-3 flex items-center justify-between">
                    <h2 className="text-2xl font-bold text-card-foreground">
                        {t('sort.title')}
                    </h2>
                    <MorphingDialogClose className="relative right-0 top-0" />
                </header>
            </MorphingDialogTitle>
            <MorphingDialogDescription className="flex-1 min-h-0 overflow-hidden flex flex-col">
                <div className="flex-1 min-h-0 overflow-y-auto">
                    <DragDropContext onDragEnd={handleDragEnd}>
                        <Droppable droppableId="sort-groups">
                            {(droppableProvided) => (
                                <div
                                    ref={droppableProvided.innerRef}
                                    {...droppableProvided.droppableProps}
                                    className="flex flex-col space-y-1.5 p-1"
                                >
                                    {sortedGroups.map((group, index) => (
                                        <Draggable
                                            key={group.id}
                                            draggableId={String(group.id)}
                                            index={index}
                                        >
                                            {(draggableProvided, snapshot) => (
                                                <SortGroupItem
                                                    group={group}
                                                    index={index}
                                                    dnd={{
                                                        innerRef: draggableProvided.innerRef,
                                                        draggableProps: draggableProvided.draggableProps,
                                                        dragHandleProps: draggableProvided.dragHandleProps,
                                                        isDragging: snapshot.isDragging,
                                                    }}
                                                />
                                            )}
                                        </Draggable>
                                    ))}
                                    {droppableProvided.placeholder}
                                </div>
                            )}
                        </Droppable>
                    </DragDropContext>
                </div>

                <div className="flex justify-end gap-2 pt-4 shrink-0">
                    <Button
                        variant="outline"
                        onClick={handleCancel}
                        className="rounded-xl"
                    >
                        {t('sort.cancel')}
                    </Button>
                    <Button
                        onClick={handleSave}
                        disabled={reorderGroups.isPending}
                        className="rounded-xl"
                    >
                        {t('sort.save')}
                    </Button>
                </div>
            </MorphingDialogDescription>
        </>
    );
}
