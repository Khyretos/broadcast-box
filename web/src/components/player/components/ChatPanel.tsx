import {
	FormEvent,
	memo,
	useCallback,
	useContext,
	useEffect,
	useRef,
	useState,
} from "react";
import {
	ChatBubbleLeftRightIcon,
	FaceSmileIcon,
	PencilSquareIcon,
	PaperAirplaneIcon,
} from "@heroicons/react/24/outline";
import { Emote, EmoteMap, StreamEmotes, isAllowedGifUrl, useEmotes } from "../../../hooks/useEmotes";
import MediaPicker from "./MediaPicker";
import { Reaction, loadSelectedReaction, saveSelectedReaction } from "../functions/reactions";
import { RecentItem, recordUse } from "../functions/recentItems";
import {
	ChatAdapter,
	ChatStatus,
	Message,
	useChatSession,
} from "../../../hooks/useChatSession";
import { LocaleContext } from "../../../providers/LocaleProvider";

const noop = () => {};

type ChatVariant = "sidebar" | "fill" | "compact-below" | "below";

interface ChatPanelProps {
	streamKey: string;
	variant: ChatVariant;
	isOpen: boolean;
	adapter?: ChatAdapter;
	displayName?: string;
	onReaction?: (reaction: Reaction) => void;
	onChangeDisplayNameRequested?: () => void;
}

// Holding the reaction button this long opens the reaction picker
const REACTION_HOLD_MS = 150;

// Emotes one message may carry, matches the server limit
const MAX_MESSAGE_EMOTES = 20;

const getNameColor = (displayName: string) => {
	let hash = 0;
	for (let i = 0; i < displayName.length; i += 1) {
		hash = displayName.charCodeAt(i) + ((hash << 5) - hash);
	}

	return `hsl(${Math.abs(hash) % 360}, 70%, 60%)`;
};

const ChatGif = (props: { url: string }) => {
	const [failed, setFailed] = useState(false);
	if (failed) {
		return <a href={props.url} target="_blank" rel="noopener noreferrer nofollow" className="break-all text-blue-300 underline">{props.url}</a>;
	}
	return (
		<img
			src={props.url}
			alt=""
			loading="lazy"
			referrerPolicy="no-referrer"
			onError={() => setFailed(true)}
			className="my-1 block max-h-40 max-w-full rounded"
		/>
	);
};

// Replaces emote codes with emote images and allowed GIF links with the GIF
const renderMessageText = (message: Message, emotes: EmoteMap, gifHosts: string[]) => {
	if (emotes.size === 0 && !message.emotes && gifHosts.length === 0) {
		return message.text;
	}

	return message.text.split(/(\s+)/).map((word, index) => {
		const url = message.emotes?.[word] ?? emotes.get(word)?.url;
		if (url && url.startsWith("https://")) {
			const emote = emotes.get(word);
			return (
				<img
					key={index}
					src={url}
					srcSet={emote?.url === url && emote.url2x ? `${emote.url} 1x, ${emote.url2x} 2x` : undefined}
					alt={word}
					title={word}
					loading="lazy"
					className="-my-1 inline-block h-7 w-auto align-middle"
				/>
			);
		}
		if (isAllowedGifUrl(word, gifHosts)) {
			return <ChatGif key={index} url={word} />;
		}
		return word;
	});
};

const ChatMessage = memo(function ChatMessage(props: { message: Message; emotes: EmoteMap; gifHosts: string[] }) {
	const { message, emotes, gifHosts } = props;
	const timestamp = new Date(message.ts).toLocaleTimeString([], {
		hour: "2-digit",
		minute: "2-digit",
	});

	return (
		<div className="bg-gray-900/40 p-2">
			<div className="flex items-center gap-2 text-xs">
				<span
					className="max-w-56 truncate font-bold"
					style={{ color: getNameColor(message.displayName) }}
				>
					{message.displayName}
				</span>
				<span className="text-gray-400">{timestamp}</span>
			</div>
			<p className="mt-1 break-words text-sm text-gray-100">{renderMessageText(message, emotes, gifHosts)}</p>
		</div>
	);
});

const ReactionPreview = (props: { reaction: Reaction }) => props.reaction.emote
	? <img src={props.reaction.emote.url} alt={props.reaction.emote.code} className="max-h-6 max-w-7 object-contain" />
	: <span className="text-xl leading-none">{props.reaction.emoji}</span>;

interface ChatComposerProps {
	status: ChatStatus;
	isSending: boolean;
	emotes: StreamEmotes;
	onNameRequested(): void;
	onReaction?: (reaction: Reaction) => void;
	onSend(text: string, emotes: Record<string, string>): Promise<boolean>;
	locale: {
		placeholder_input: string;
		button_reaction_hold_hint: string;
		button_change_display_name_title: string;
		button_send_title: string;
		button_emoji_title: string;
	};
}

const ChatComposer = memo(function ChatComposer(props: ChatComposerProps) {
	const { status, isSending, emotes, onNameRequested, onReaction, onSend, locale } = props;
	const [text, setText] = useState("");
	const [picker, setPicker] = useState<"chat" | "reaction">();
	const [reaction, setReaction] = useState<Reaction>(loadSelectedReaction);
	const inputRef = useRef<HTMLInputElement>(null);
	const composerRef = useRef<HTMLFormElement>(null);
	// Emotes picked from search, which other viewers may not have loaded
	const pickedEmotesRef = useRef(new Map<string, Emote>());
	const holdTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
	const pressRef = useRef<"idle" | "pressed" | "held">("idle");
	const closePicker = useCallback(() => setPicker(undefined), []);
	const canSend =
		text.trim().length > 0 && !isSending && status === "connected";

	// Inserts at the cursor, with spaces around emote codes and links so they are recognised
	const insertText = (value: string, isWord: boolean) => {
		const input = inputRef.current;
		const selectionStart = input?.selectionStart ?? text.length;
		const selectionEnd = input?.selectionEnd ?? text.length;
		const before = text.slice(0, selectionStart);
		const after = text.slice(selectionEnd);
		const insert = isWord
			? `${before && !/\s$/.test(before) ? " " : ""}${value}${/^\s/.test(after) ? "" : " "}`
			: value;
		const next = (before + insert + after).slice(0, 2000);
		setText(next);

		requestAnimationFrame(() => {
			input?.focus();
			const caret = Math.min(before.length + insert.length, next.length);
			input?.setSelectionRange(caret, caret);
		});
	};

	const onPick = (item: RecentItem) => {
		// GIFs are counted when the message is sent, so pasted links count too
		if (item.kind !== "gif") {
			recordUse(item);
		}

		if (picker === "reaction") {
			if (item.kind === "emoji") {
				setReaction({ emoji: item.emoji });
				saveSelectedReaction({ emoji: item.emoji });
			} else if (item.kind === "emote") {
				const selected: Reaction = { emote: { code: item.emote.code, url: item.emote.url } };
				setReaction(selected);
				saveSelectedReaction(selected);
			}
			setPicker(undefined);
			return;
		}

		switch (item.kind) {
			case "emoji":
				insertText(item.emoji, false);
				break;
			case "emote":
				pickedEmotesRef.current.set(item.emote.code, item.emote);
				insertText(item.emote.code, true);
				break;
			case "gif":
				// Like Discord, a picked GIF is sent right away as its own message
				setPicker(undefined);
				void onSend(item.url, {}).then((sent) => sent && recordUse(item));
				break;
		}
	};

	// Emote code to image URL for every emote in the message
	const messageEmotes = (message: string) => {
		const found: Record<string, string> = {};
		for (const word of message.split(/\s+/)) {
			const emote = pickedEmotesRef.current.get(word) ?? emotes.map.get(word);
			if (emote && Object.keys(found).length < MAX_MESSAGE_EMOTES) {
				found[word] = emote.url;
			}
		}
		return found;
	};

	const submit = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();

		const message = text.trim();
		if (!message) {
			return;
		}

		const sent = await onSend(message, messageEmotes(message));
		if (sent) {
			for (const word of message.split(/\s+/)) {
				if (isAllowedGifUrl(word, emotes.gifHosts)) {
					recordUse({ kind: "gif", url: word });
				}
			}
			setText("");
		}
	};

	// Click sends the selected reaction, holding opens the reaction picker
	const cancelHold = () => {
		clearTimeout(holdTimerRef.current);
		pressRef.current = "idle";
	};
	const onReactionPointerDown = (event: React.PointerEvent) => {
		if (!onReaction || event.button !== 0) {
			return;
		}
		pressRef.current = "pressed";
		clearTimeout(holdTimerRef.current);
		holdTimerRef.current = setTimeout(() => {
			pressRef.current = "held";
			setPicker("reaction");
		}, REACTION_HOLD_MS);
	};
	const onReactionPointerUp = () => {
		clearTimeout(holdTimerRef.current);
		if (pressRef.current === "pressed") {
			onReaction?.(reaction);
		}
		pressRef.current = "idle";
	};

	useEffect(() => () => clearTimeout(holdTimerRef.current), []);

	return (
		<form
			ref={composerRef}
			onSubmit={submit}
			className="border-t border-gray-700 bg-gray-900/70 p-3"
		>
			<div className="relative flex items-center gap-2">
				{picker && (
					<MediaPicker anchorRef={composerRef} mode={picker} emotes={emotes} onPick={onPick} onClose={closePicker} />
				)}

				<button
					type="button"
					onPointerDown={onReactionPointerDown}
					onPointerUp={onReactionPointerUp}
					onPointerLeave={cancelHold}
					onPointerCancel={cancelHold}
					onContextMenu={(event) => event.preventDefault()}
					onKeyDown={(event) => {
						if (event.key === "Enter" || event.key === " ") {
							event.preventDefault();
							onReaction?.(reaction);
						} else if (event.key === "ArrowUp") {
							event.preventDefault();
							setPicker("reaction");
						}
					}}
					disabled={!onReaction}
					className={`inline-flex h-9 w-9 shrink-0 touch-none select-none items-center justify-center rounded-md border border-gray-700 hover:bg-gray-700 active:scale-95 disabled:cursor-not-allowed disabled:opacity-40 ${picker === "reaction" ? "bg-gray-700" : "bg-gray-800"}`}
					title={locale.button_reaction_hold_hint}
					aria-label={locale.button_reaction_hold_hint}
				>
					<ReactionPreview reaction={reaction} />
				</button>

				<button
					type="button"
					onClick={() => setPicker((open) => open === "chat" ? undefined : "chat")}
					className={`inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-gray-700 hover:bg-gray-700 ${picker === "chat" ? "bg-gray-700 text-yellow-300" : "bg-gray-800 text-gray-100"}`}
					title={locale.button_emoji_title}
				>
					<FaceSmileIcon className="h-5 w-5" />
				</button>

				<input
					ref={inputRef}
					type="text"
					value={text}
					maxLength={2000}
					onChange={(event) => setText(event.target.value)}
					placeholder={locale.placeholder_input}
					className="h-9 min-w-0 flex-1 rounded-md border border-gray-700 bg-gray-800 px-3 text-sm text-gray-100 placeholder:text-gray-400 focus:outline-hidden"
				/>

				<button
					type="button"
					onClick={onNameRequested}
					className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-gray-700 bg-gray-800 text-gray-100 hover:bg-gray-700"
					title={locale.button_change_display_name_title}
				>
					<PencilSquareIcon className="h-5 w-5" />
				</button>

				<button
					type="submit"
					disabled={!canSend}
					className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-blue-600 text-white disabled:cursor-not-allowed disabled:bg-gray-700 disabled:text-gray-400"
					title={locale.button_send_title}
				>
					<PaperAirplaneIcon className="h-5 w-5" />
				</button>
			</div>

			{text.length > 1800 && (
				<div className="mt-1 text-right text-xs text-gray-400">
					{text.length}/2000
				</div>
			)}
		</form>
	);
});

const statusColorClass = (status: ChatStatus) => {
	if (status === "connected") {
		return "bg-green-500";
	}

	if (status === "connecting") {
		return "animate-pulse bg-yellow-400";
	}

	return "bg-red-500";
};

const getLocalizedStatus = (status: ChatStatus, locale: { status_connecting: string; status_connected: string; status_error: string; status_disconnected: string }) => {
	switch (status) {
		case "connecting":
			return locale.status_connecting;
		case "connected":
			return locale.status_connected;
		case "error":
			return locale.status_error;
		case "disconnected":
			return locale.status_disconnected;
		default:
			return status;
	}
};

const ChatPanel = (props: ChatPanelProps) => {
	const { streamKey, variant, isOpen, adapter, displayName, onReaction, onChangeDisplayNameRequested } = props;
	const { locale } = useContext(LocaleContext);
	const streamEmotes = useEmotes(streamKey);
	const { messages, status, error, sendMessage } = useChatSession(
		streamKey,
		adapter,
		locale.chat.error_failed_to_connect,
	);

	const [isSending, setIsSending] = useState(false);
	const [sendError, setSendError] = useState<string | null>(null);

	const messageListRef = useRef<HTMLDivElement>(null);
	const shouldStickToBottomRef = useRef(true);
	const firstBatchRef = useRef(true);

	useEffect(() => {
		if (!isOpen) {
			return;
		}

		const node = messageListRef.current;
		if (!node) {
			return;
		}

		if (firstBatchRef.current || shouldStickToBottomRef.current) {
			node.scrollTop = node.scrollHeight;
			firstBatchRef.current = false;
		}
	}, [isOpen, messages]);

	useEffect(() => {
		firstBatchRef.current = true;
		shouldStickToBottomRef.current = true;
	}, [streamKey]);

	const onMessageListScroll = () => {
		const node = messageListRef.current;
		if (!node) {
			return;
		}

		const distanceToBottom =
			node.scrollHeight - node.scrollTop - node.clientHeight;
		shouldStickToBottomRef.current = distanceToBottom <= 100;
	};

	const onSend = useCallback(
		async (text: string, emotes: Record<string, string>) => {
			if (!displayName?.trim()) {
				onChangeDisplayNameRequested?.();
				return false;
			}

			setIsSending(true);
			setSendError(null);

			try {
				await sendMessage(text.trim(), displayName!.trim(), locale.chat.error_not_connected, emotes);
				return true;
			} catch (nextError) {
				const message =
					nextError instanceof Error
						? (nextError.message.includes("too fast") ? locale.chat.error_rate_limited : nextError.message)
						: locale.chat.error_failed_to_send;
				setSendError(message);
				return false;
			} finally {
				setIsSending(false);
			}
		},
		[displayName, sendMessage, onChangeDisplayNameRequested, locale.chat.error_failed_to_send, locale.chat.error_not_connected, locale.chat.error_rate_limited],
	);

	const base =
		"flex flex-col overflow-hidden rounded-md border border-gray-700 bg-slate-900 text-gray-100 transition-[height,max-height,width,opacity,transform,border-color] duration-200 ease-out";
	const belowHeightClass = variant === "compact-below" ? "h-80" : "h-96";
	const panelClassName = variant === "fill"
		? `${base} ${isOpen ? "min-h-0 flex-1 opacity-100" : "hidden"}`
		: variant === "sidebar"
		? `${base} min-h-0 shrink-0 ${
			isOpen
				? "absolute top-0 right-0 h-full w-80 opacity-100"
				: "absolute top-0 right-0 h-full w-0 max-h-none translate-x-2 translate-y-0 opacity-0 pointer-events-none border-transparent"
		}`
		: `${base} ${isOpen ? `${belowHeightClass} translate-y-0 opacity-100` : "h-0 translate-y-1 border-transparent opacity-0 pointer-events-none"}`;

	return (
		<div
			className={panelClassName}
		>
			<div className="flex items-center justify-between border-b border-gray-700 bg-gray-900/70 px-3 py-2">
				<div className="flex items-center gap-2 text-sm font-semibold">
					<ChatBubbleLeftRightIcon className="h-4 w-4" />
					<span>{locale.chat.title}</span>
				</div>

				<div className="flex items-center gap-2 text-xs text-gray-300">
					<span
						className={`inline-flex h-2.5 w-2.5 rounded-full ${statusColorClass(status)}`}
					/>
					<span className="capitalize">{getLocalizedStatus(status, locale.chat)}</span>
				</div>
			</div>

			<div
				ref={messageListRef}
				onScroll={onMessageListScroll}
				style={{ colorScheme: "dark" }}
				className="min-h-0 flex-1 overflow-y-auto px-3 py-2"
			>
				{error && (
					<div className="mb-2 rounded-md border border-red-400 bg-red-950/40 px-2 py-1 text-xs text-red-200">
						{error}
					</div>
				)}
				{sendError && (
					<div className="mb-2 rounded-md border border-red-400 bg-red-950/40 px-2 py-1 text-xs text-red-200">
						{sendError}
					</div>
				)}

				{!error && messages.length === 0 && status === "connected" && (
					<div className="text-xs text-gray-400">{locale.chat.no_messages_yet}</div>
				)}

				<div className="space-y-0">
					{messages.map((message) => (
						<ChatMessage key={message.id} message={message} emotes={streamEmotes.map} gifHosts={streamEmotes.gifHosts} />
					))}
				</div>
			</div>

			<ChatComposer
				status={status}
				isSending={isSending}
				emotes={streamEmotes}
				onNameRequested={onChangeDisplayNameRequested ?? noop}
				onReaction={onReaction}
				onSend={onSend}
				locale={locale.chat}
			/>
		</div>
	);
};

export default ChatPanel;
