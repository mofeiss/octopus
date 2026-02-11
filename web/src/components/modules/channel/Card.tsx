import {
    MorphingDialog,
    MorphingDialogTrigger,
    MorphingDialogContainer,
    MorphingDialogContent,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { DollarSign, Info, MessageSquare, Pencil, Trash2 } from 'lucide-react';
import { type StatsMetricsFormatted } from '@/api/endpoints/stats';
import { type Channel, useEnableChannel, useUpdateChannel } from '@/api/endpoints/channel';
import { CardContent } from './CardContent';
import { useTranslations } from 'next-intl';
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/animate-ui/components/animate/tooltip';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/common/Toast';
import { useState, useRef } from 'react';
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogFooter,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';

export function Card({ channel, stats }: { channel: Channel; stats: StatsMetricsFormatted }) {
    const t = useTranslations('channel.card');
    const enableChannel = useEnableChannel();
    const updateChannel = useUpdateChannel();

    // [fork] ref to control whether CardContent opens in editing or viewing mode
    const defaultEditingRef = useRef(true);

    // [fork] remark edit/delete modal state
    const [editRemarkOpen, setEditRemarkOpen] = useState(false);
    const [deleteRemarkOpen, setDeleteRemarkOpen] = useState(false);
    const [remarkDraft, setRemarkDraft] = useState('');

    const handleEnableChange = (checked: boolean) => {
        enableChannel.mutate(
            { id: channel.id, enabled: checked },
            {
                onSuccess: () => {
                    toast.success(checked ? t('toast.enabled') : t('toast.disabled'));
                },
                onError: (error) => {
                    toast.error(error.message);
                },
            }
        );
    };

    // [fork] quick edit remark
    const handleEditRemarkOpen = (e: React.MouseEvent) => {
        e.stopPropagation();
        setRemarkDraft(channel.remark ?? '');
        setEditRemarkOpen(true);
    };

    const handleEditRemarkSave = () => {
        updateChannel.mutate(
            { id: channel.id, remark: remarkDraft },
            {
                onSuccess: () => {
                    setEditRemarkOpen(false);
                },
            }
        );
    };

    // [fork] quick delete remark
    const handleDeleteRemarkOpen = (e: React.MouseEvent) => {
        e.stopPropagation();
        setDeleteRemarkOpen(true);
    };

    const handleDeleteRemarkConfirm = () => {
        updateChannel.mutate(
            { id: channel.id, remark: '' },
            {
                onSuccess: () => {
                    setDeleteRemarkOpen(false);
                },
            }
        );
    };

    return (
        <>
            <MorphingDialog>
                <MorphingDialogTrigger className="w-full">
                    <article onClickCapture={() => { defaultEditingRef.current = true; }} className="relative flex min-h-54 flex-col justify-between gap-5 rounded-3xl border border-border bg-card text-card-foreground p-4 custom-shadow transition-all duration-300 hover:scale-[1.02]">
                        <header className="relative flex items-center justify-between gap-2">
                            <Tooltip side="top" sideOffset={10} align="center">
                                <TooltipTrigger asChild>
                                    <h3 className="text-lg font-bold truncate min-w-0">{channel.name}</h3>
                                </TooltipTrigger>
                                <TooltipContent key={channel.name}>{channel.name}</TooltipContent>
                            </Tooltip>
                            <div className="flex items-center gap-1 shrink-0">
                                <InfoButton onClick={() => { defaultEditingRef.current = false; }} />
                                <Switch
                                    checked={channel.enabled}
                                    onCheckedChange={handleEnableChange}
                                    disabled={enableChannel.isPending}
                                    onClick={(e) => e.stopPropagation()}
                                />
                            </div>
                        </header>

                        {/* [fork] remark display row */}
                        {channel.remark && (
                            <div className="relative flex items-center justify-between gap-2 -mt-3">
                                <span className="text-sm text-primary truncate min-w-0">{channel.remark}</span>
                                <div className="flex items-center gap-1 shrink-0">
                                    <button
                                        type="button"
                                        onClick={handleEditRemarkOpen}
                                        className="p-1 rounded-md text-muted-foreground/50 hover:text-muted-foreground transition-colors"
                                    >
                                        <Pencil className="h-3.5 w-3.5" />
                                    </button>
                                    <button
                                        type="button"
                                        onClick={handleDeleteRemarkOpen}
                                        className="p-1 rounded-md text-muted-foreground/50 hover:text-destructive transition-colors"
                                    >
                                        <Trash2 className="h-3.5 w-3.5" />
                                    </button>
                                </div>
                            </div>
                        )}

                        <dl className="relative grid grid-cols-1 gap-3">
                            <div className="flex items-center justify-between rounded-2xl border border-border/70 bg-background/80 p-2">
                                <div className="flex items-center gap-3">
                                    <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
                                        <MessageSquare className="h-5 w-5" />
                                    </span>
                                    <dt className="text-sm text-muted-foreground">{t('requestCount')}</dt>
                                </div>
                                <dd className="text-base">
                                    {stats.request_count.formatted.value}
                                    <span className="ml-1 text-xs text-muted-foreground">{stats.request_count.formatted.unit}</span>
                                </dd>
                            </div>

                            <div className="flex items-center justify-between rounded-2xl border border-border/70 bg-background/80 p-2">
                                <div className="flex items-center gap-3">
                                    <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
                                        <DollarSign className="h-5 w-5" />
                                    </span>
                                    <dt className="text-sm text-muted-foreground">{t('totalCost')}</dt>
                                </div>
                                <dd className="text-base">
                                    {stats.total_cost.formatted.value}
                                    <span className="ml-1 text-xs text-muted-foreground">{stats.total_cost.formatted.unit}</span>
                                </dd>
                            </div>
                        </dl>
                    </article>
                </MorphingDialogTrigger>

                <MorphingDialogContainer>
                    <MorphingDialogContent className="w-full md:max-w-xl bg-card text-card-foreground px-4 py-2 custom-shadow rounded-3xl max-h-[90vh] overflow-y-auto">
                        <CardContent channel={channel} stats={stats} initialEditing={defaultEditingRef.current} />
                    </MorphingDialogContent>
                </MorphingDialogContainer>
            </MorphingDialog>

            {/* [fork] Edit remark dialog */}
            <Dialog open={editRemarkOpen} onOpenChange={setEditRemarkOpen}>
                <DialogContent className="sm:max-w-md rounded-2xl">
                    <DialogHeader>
                        <DialogTitle>{t('editRemark')}</DialogTitle>
                    </DialogHeader>
                    <Input
                        value={remarkDraft}
                        onChange={(e) => setRemarkDraft(e.target.value)}
                        placeholder={t('editRemarkPlaceholder')}
                        className="rounded-xl"
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                                e.preventDefault();
                                handleEditRemarkSave();
                            }
                        }}
                    />
                    <DialogFooter>
                        <Button
                            onClick={handleEditRemarkSave}
                            disabled={updateChannel.isPending}
                            className="rounded-xl"
                        >
                            {t('editRemarkSave')}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* [fork] Delete remark dialog */}
            <Dialog open={deleteRemarkOpen} onOpenChange={setDeleteRemarkOpen}>
                <DialogContent className="sm:max-w-md rounded-2xl">
                    <DialogHeader>
                        <DialogTitle>{t('deleteRemark')}</DialogTitle>
                    </DialogHeader>
                    <p className="text-sm text-muted-foreground">{t('deleteRemarkConfirm')}</p>
                    <DialogFooter>
                        <Button
                            variant="secondary"
                            onClick={() => setDeleteRemarkOpen(false)}
                            className="rounded-xl"
                        >
                            {t('cancel')}
                        </Button>
                        <Button
                            variant="destructive"
                            onClick={handleDeleteRemarkConfirm}
                            disabled={updateChannel.isPending}
                            className="rounded-xl"
                        >
                            {t('confirm')}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    );
}

// [fork] InfoButton must be inside MorphingDialog to access useMorphingDialog()
function InfoButton({ onClick }: { onClick: () => void }) {
    const { setIsOpen } = useMorphingDialog();
    const t = useTranslations('channel.card');
    return (
        <Tooltip side="top" sideOffset={10} align="center">
            <TooltipTrigger asChild>
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        onClick();
                        setIsOpen(true);
                    }}
                    className="p-1 rounded-md text-muted-foreground/50 hover:text-muted-foreground transition-colors"
                >
                    <Info className="h-4 w-4" />
                </button>
            </TooltipTrigger>
            <TooltipContent>{t('info')}</TooltipContent>
        </Tooltip>
    );
}
