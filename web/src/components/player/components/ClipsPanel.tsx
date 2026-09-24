import { memo, useCallback, useContext, useEffect, useState } from "react";
import { ArrowDownTrayIcon, FilmIcon, LinkIcon, TrashIcon } from "@heroicons/react/24/outline";
import { LocaleContext } from "../../../providers/LocaleProvider";
import { Clip, canDeleteClips, clipVideoUrl, deleteClip, formatClipTime, listClips, onClipsUpdated } from "../functions/clipsApi";

interface ClipsPanelProps {
	streamKey: string;
	className?: string;
}

const REFRESH_INTERVAL_MS = 30_000;

const ClipItem = memo(function ClipItem(props: { clip: Clip; isExpanded: boolean; onToggle(): void }) {
	const { clip, isExpanded, onToggle } = props;
	const { locale } = useContext(LocaleContext);
	const [isCopied, setIsCopied] = useState(false);
	const createdAt = new Date(clip.createdAt);

	const copyLink = async () => {
		await navigator.clipboard?.writeText(new URL(clipVideoUrl(clip), window.location.href).toString());
		setIsCopied(true);
		setTimeout(() => setIsCopied(false), 1500);
	};

	const remove = async () => {
		if (window.confirm(locale.clips.confirm_delete)) {
			await deleteClip(clip).catch((error) => console.error("ClipsPanel.Delete", error));
		}
	};

	return (
		<div className="bg-gray-900/40 p-2">
			<button onClick={onToggle} className="w-full text-left">
				<div className="flex items-center justify-between gap-2">
					<span className="truncate text-sm font-semibold">{clip.title}</span>
					<span className="shrink-0 rounded bg-gray-800 px-1.5 font-mono text-xs text-gray-300">
						{formatClipTime(clip.durationSeconds)}
					</span>
				</div>
				<div className="text-xs text-gray-400">
					{createdAt.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}
					{clip.height ? ` · ${clip.height}p` : ""}
				</div>
			</button>

			{isExpanded && (
				<div className="mt-2 flex flex-col gap-2">
					<video className="aspect-video w-full rounded bg-gray-950" src={clipVideoUrl(clip)} controls autoPlay playsInline />
					<div className="flex gap-1 text-xs">
						<a href={clipVideoUrl(clip, true)} className="flex items-center gap-1 rounded bg-gray-800 px-2 py-1 hover:bg-gray-700">
							<ArrowDownTrayIcon className="h-4 w-4" />
							{locale.clips.button_download}
						</a>
						<button onClick={() => void copyLink()} className="flex items-center gap-1 rounded bg-gray-800 px-2 py-1 hover:bg-gray-700">
							<LinkIcon className="h-4 w-4" />
							{isCopied ? locale.clips.link_copied : locale.clips.button_copy_link}
						</button>
						{canDeleteClips() && (
							<button onClick={() => void remove()} className="ml-auto flex items-center gap-1 rounded bg-red-900/60 px-2 py-1 hover:bg-red-800">
								<TrashIcon className="h-4 w-4" />
								{locale.clips.button_delete}
							</button>
						)}
					</div>
				</div>
			)}
		</div>
	);
});

const ClipsPanel = (props: ClipsPanelProps) => {
	const { streamKey, className = "" } = props;
	const { locale } = useContext(LocaleContext);
	const [clips, setClips] = useState<Clip[] | undefined>();
	const [error, setError] = useState(false);
	const [expandedId, setExpandedId] = useState<string>();

	const refresh = useCallback(() => {
		listClips(streamKey)
			.then((result) => {
				setClips(result);
				setError(false);
			})
			.catch(() => setError(true));
	}, [streamKey]);

	useEffect(() => {
		refresh();
		const interval = setInterval(refresh, REFRESH_INTERVAL_MS);
		const unsubscribe = onClipsUpdated(streamKey, refresh);
		return () => {
			clearInterval(interval);
			unsubscribe();
		};
	}, [refresh, streamKey]);

	return (
		<div className={`flex flex-col overflow-hidden rounded-md border border-gray-700 bg-slate-900 text-gray-100 ${className}`}>
			<div className="flex items-center justify-between border-b border-gray-700 bg-gray-900/70 px-3 py-2">
				<div className="flex items-center gap-2 text-sm font-semibold">
					<FilmIcon className="h-4 w-4" />
					<span>{locale.clips.title}</span>
				</div>
				{clips && <span className="text-xs text-gray-400">{clips.length}</span>}
			</div>

			<div style={{ colorScheme: "dark" }} className="min-h-0 flex-1 space-y-px overflow-y-auto">
				{error && <div className="p-3 text-xs text-red-300">{locale.clips.error_loading}</div>}
				{!error && clips === undefined && <div className="p-3 text-xs text-gray-400">{locale.clips.loading}</div>}
				{clips?.length === 0 && <div className="p-3 text-xs text-gray-400">{locale.clips.no_clips_yet}</div>}
				{clips?.map((clip) => (
					<ClipItem
						key={clip.id}
						clip={clip}
						isExpanded={expandedId === clip.id}
						onToggle={() => setExpandedId((current) => current === clip.id ? undefined : clip.id)}
					/>
				))}
			</div>
		</div>
	);
};

export default ClipsPanel;
