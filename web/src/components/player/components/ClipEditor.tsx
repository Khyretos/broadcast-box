import { useCallback, useContext, useEffect, useRef, useState } from "react";
import { PlayIcon, ScissorsIcon, XMarkIcon } from "@heroicons/react/20/solid";
import { LocaleContext } from "../../../providers/LocaleProvider";
import {
	Clip,
	ClipDraft,
	ClipError,
	clipDraftVideoUrl,
	clipVideoUrl,
	createClipDraft,
	defaultClipTitle,
	formatClipTime,
	publishClip,
} from "../functions/clipsApi";

interface ClipEditorProps {
	streamKey: string;
	onClose(): void;
}

// Selection shown when the editor opens, like Twitch: the last 30 seconds
const DEFAULT_CLIP_SECONDS = 30;
const MIN_CLIP_SECONDS = 1;

type EditorState =
	| { kind: "loading" }
	| { kind: "error"; message: string }
	| { kind: "editing"; draft: ClipDraft }
	| { kind: "published"; clip: Clip };

const ClipEditor = (props: ClipEditorProps) => {
	const { streamKey, onClose } = props;
	const { locale } = useContext(LocaleContext);
	const [state, setState] = useState<EditorState>({ kind: "loading" });
	const [start, setStart] = useState(0);
	const [end, setEnd] = useState(0);
	const [title, setTitle] = useState("");
	const [isPublishing, setIsPublishing] = useState(false);
	const [publishError, setPublishError] = useState<string>();
	const [isPreviewUnsupported, setIsPreviewUnsupported] = useState(false);
	const [defaultTitle] = useState(() => defaultClipTitle());
	const videoRef = useRef<HTMLVideoElement>(null);
	const playUntilRef = useRef<number | undefined>(undefined);

	const errorMessage = useCallback((error: unknown) => {
		if (error instanceof ClipError) {
			switch (error.kind) {
				case "nothing_recorded":
					return locale.clips.error_nothing_recorded;
				case "rate_limited":
					return locale.clips.error_rate_limited;
				case "draft_expired":
					return locale.clips.error_draft_expired;
			}
		}
		return locale.clips.error_generic;
	}, [locale]);

	useEffect(() => {
		let cancelled = false;
		createClipDraft(streamKey)
			.then((draft) => {
				if (cancelled) {
					return;
				}
				setStart(Math.max(0, draft.durationSeconds - Math.min(DEFAULT_CLIP_SECONDS, draft.maxClipSeconds)));
				setEnd(draft.durationSeconds);
				setState({ kind: "editing", draft });
			})
			.catch((error) => !cancelled && setState({ kind: "error", message: errorMessage(error) }));

		return () => {
			cancelled = true;
		};
	}, [errorMessage, streamKey]);

	useEffect(() => {
		const onKeyDown = (event: KeyboardEvent) => event.key === "Escape" && onClose();
		window.addEventListener("keydown", onKeyDown);
		return () => window.removeEventListener("keydown", onKeyDown);
	}, [onClose]);

	const draft = state.kind === "editing" ? state.draft : undefined;
	const duration = draft?.durationSeconds ?? 0;
	const maxLength = Math.min(draft?.maxClipSeconds ?? duration, duration);

	const seek = (seconds: number) => {
		const video = videoRef.current;
		if (video && !isPreviewUnsupported) {
			playUntilRef.current = undefined;
			video.pause();
			video.currentTime = seconds;
		}
	};

	const changeStart = (value: number) => {
		const nextStart = Math.min(value, duration - MIN_CLIP_SECONDS);
		let nextEnd = end;
		if (nextEnd - nextStart < MIN_CLIP_SECONDS) {
			nextEnd = nextStart + MIN_CLIP_SECONDS;
		}
		if (nextEnd - nextStart > maxLength) {
			nextEnd = nextStart + maxLength;
		}
		setStart(nextStart);
		setEnd(Math.min(nextEnd, duration));
		seek(nextStart);
	};

	const changeEnd = (value: number) => {
		const nextEnd = Math.max(value, MIN_CLIP_SECONDS);
		let nextStart = start;
		if (nextEnd - nextStart < MIN_CLIP_SECONDS) {
			nextStart = nextEnd - MIN_CLIP_SECONDS;
		}
		if (nextEnd - nextStart > maxLength) {
			nextStart = nextEnd - maxLength;
		}
		setStart(Math.max(0, nextStart));
		setEnd(nextEnd);
		seek(nextEnd);
	};

	const playSelection = () => {
		const video = videoRef.current;
		if (!video) {
			return;
		}
		video.currentTime = start;
		playUntilRef.current = end;
		void video.play();
	};

	const onTimeUpdate = () => {
		const video = videoRef.current;
		if (video && playUntilRef.current !== undefined && video.currentTime >= playUntilRef.current) {
			video.pause();
			playUntilRef.current = undefined;
		}
	};

	const publish = async () => {
		if (!draft) {
			return;
		}
		setIsPublishing(true);
		setPublishError(undefined);
		try {
			const clip = await publishClip(draft.id, start, end, title.trim() || defaultTitle);
			setState({ kind: "published", clip });
		} catch (error) {
			setPublishError(errorMessage(error));
		} finally {
			setIsPublishing(false);
		}
	};

	const percent = (seconds: number) => duration > 0 ? `${(seconds / duration) * 100}%` : "0%";

	return (
		<div className="fixed inset-0 z-[100] flex items-center justify-center p-4">
			<div className="absolute inset-0 bg-black/70" onClick={onClose} />

			<div className="relative flex max-h-full w-full max-w-3xl flex-col gap-3 overflow-y-auto rounded-lg bg-gray-800 p-4 text-gray-100 shadow-xl">
				<div className="flex items-center justify-between">
					<h2 className="flex items-center gap-2 text-lg font-semibold">
						<ScissorsIcon className="h-5 w-5" />
						{state.kind === "published" ? locale.clips.editor_published : locale.clips.editor_title}
					</h2>
					<button onClick={onClose} className="rounded-full p-1 hover:bg-gray-700" title={locale.clips.editor_close}>
						<XMarkIcon className="h-5 w-5" />
					</button>
				</div>

				{state.kind === "loading" && (
					<div className="flex aspect-video items-center justify-center rounded-md bg-gray-950 text-sm text-gray-300">
						<span className="animate-pulse">{locale.clips.editor_preparing}</span>
					</div>
				)}

				{state.kind === "error" && (
					<div className="rounded-md border border-red-400 bg-red-950/40 p-3 text-sm text-red-200">{state.message}</div>
				)}

				{state.kind === "published" && (
					<>
						<video className="aspect-video w-full rounded-md bg-gray-950" src={clipVideoUrl(state.clip)} controls autoPlay playsInline />
						<p className="font-semibold">{state.clip.title}</p>
						<div className="flex justify-end gap-2">
							<a href={clipVideoUrl(state.clip, true)} className="rounded-md bg-gray-700 px-3 py-2 text-sm hover:bg-gray-600">
								{locale.clips.button_download}
							</a>
							<button onClick={onClose} className="rounded-md bg-blue-600 px-3 py-2 text-sm hover:bg-blue-500">
								{locale.clips.editor_close}
							</button>
						</div>
					</>
				)}

				{draft && (
					<>
						{isPreviewUnsupported ? (
							<div className="flex aspect-video items-center justify-center rounded-md bg-gray-950 p-6 text-center text-sm text-gray-300">
								{locale.clips.editor_preview_unsupported}
							</div>
						) : (
							<video
								ref={videoRef}
								className="aspect-video w-full rounded-md bg-gray-950"
								src={clipDraftVideoUrl(draft.id)}
								controls
								playsInline
								preload="auto"
								onLoadedMetadata={(event) => {
									event.currentTarget.currentTime = start;
								}}
								onTimeUpdate={onTimeUpdate}
								onError={() => setIsPreviewUnsupported(true)}
							/>
						)}

						{/* Selection: two range inputs sharing one track */}
						<div>
							<div className="mb-1 flex justify-between text-xs text-gray-300">
								<span>{locale.clips.editor_selection}</span>
								<span>
									{locale.clips.editor_length}: <span className="font-mono">{formatClipTime(end - start)}</span>
								</span>
							</div>
							<div className="relative h-8">
								<div className="absolute top-1/2 h-2 w-full -translate-y-1/2 rounded-full bg-gray-700" />
								<div
									className="absolute top-1/2 h-2 -translate-y-1/2 rounded-full bg-purple-500"
									style={{ left: percent(start), width: `calc(${percent(end)} - ${percent(start)})` }}
								/>
								<input
									type="range"
									aria-label={locale.clips.editor_start}
									min={0}
									max={duration}
									step={0.1}
									value={start}
									onChange={(event) => changeStart(Number(event.target.value))}
									className="clip-range-thumb pointer-events-none absolute inset-0 h-8 w-full appearance-none bg-transparent"
								/>
								<input
									type="range"
									aria-label={locale.clips.editor_end}
									min={0}
									max={duration}
									step={0.1}
									value={end}
									onChange={(event) => changeEnd(Number(event.target.value))}
									className="clip-range-thumb pointer-events-none absolute inset-0 h-8 w-full appearance-none bg-transparent"
								/>
							</div>
							<div className="mt-1 flex justify-between font-mono text-xs text-gray-300">
								<span>{locale.clips.editor_start} {formatClipTime(start)}</span>
								<span>{locale.clips.editor_end} {formatClipTime(end)}</span>
							</div>
							{maxLength < duration && (
								<p className="mt-1 text-xs text-gray-400">
									{locale.clips.editor_max_length.replace("{seconds}", String(Math.round(maxLength)))}
								</p>
							)}
						</div>

						<div className="flex flex-col gap-1">
							<label className="text-sm text-gray-300" htmlFor="clip-title">{locale.clips.editor_title_label}</label>
							<input
								id="clip-title"
								type="text"
								maxLength={100}
								value={title}
								onChange={(event) => setTitle(event.target.value)}
								onKeyDown={(event) => event.key === "Enter" && !isPublishing && void publish()}
								placeholder={`${defaultTitle} — ${locale.clips.editor_title_placeholder}`}
								className="h-10 rounded-md border border-gray-700 bg-gray-900 px-3 text-sm placeholder:text-gray-500 focus:outline-hidden"
							/>
						</div>

						{publishError && (
							<div className="rounded-md border border-red-400 bg-red-950/40 p-2 text-sm text-red-200">{publishError}</div>
						)}

						<div className="flex flex-wrap justify-end gap-2">
							{!isPreviewUnsupported && (
								<button onClick={playSelection} className="mr-auto flex items-center gap-1 rounded-md bg-gray-700 px-3 py-2 text-sm hover:bg-gray-600">
									<PlayIcon className="h-4 w-4" />
									{locale.clips.editor_preview_selection}
								</button>
							)}
							<button onClick={onClose} className="rounded-md bg-gray-700 px-3 py-2 text-sm hover:bg-gray-600">
								{locale.clips.editor_cancel}
							</button>
							<button
								onClick={() => void publish()}
								disabled={isPublishing}
								className="flex items-center gap-1 rounded-md bg-purple-600 px-3 py-2 text-sm font-semibold hover:bg-purple-500 disabled:cursor-wait disabled:opacity-60"
							>
								<ScissorsIcon className="h-4 w-4" />
								{isPublishing ? locale.clips.editor_publishing : locale.clips.editor_publish}
							</button>
						</div>
					</>
				)}
			</div>
		</div>
	);
};

export default ClipEditor;
