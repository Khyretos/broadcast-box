import { RefObject, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { LocaleContext } from "../../../providers/LocaleProvider";
import { EMOJI_CATEGORIES } from "../functions/emojiData";
import { Emote } from "../../../hooks/useEmotes";

interface EmojiPickerProps {
	// The picker opens above this element
	anchorRef: RefObject<HTMLElement | null>;
	emotes: Emote[];
	onPick(text: string): void;
	onClose(): void;
}

// Keeps the picker fast with large emote sets
const MAX_EMOTES_SHOWN = 300;

const EmojiPicker = (props: EmojiPickerProps) => {
	const { anchorRef, emotes, onPick, onClose } = props;
	const [position, setPosition] = useState<{ left: number; bottom: number; width: number; height: number }>();

	// Rendered in a portal with fixed positioning, so the chat panel's
	// overflow can't clip it
	useLayoutEffect(() => {
		const update = () => {
			const rect = anchorRef.current?.getBoundingClientRect();
			if (!rect) {
				return;
			}
			const width = Math.min(384, window.innerWidth - 16);
			setPosition({
				left: Math.max(8, Math.min(rect.left, window.innerWidth - width - 8)),
				bottom: window.innerHeight - rect.top + 8,
				width,
				height: Math.max(200, Math.min(360, rect.top - 16)),
			});
		};
		update();
		window.addEventListener("resize", update);
		window.addEventListener("scroll", update, true);
		return () => {
			window.removeEventListener("resize", update);
			window.removeEventListener("scroll", update, true);
		};
	}, [anchorRef]);
	const { locale } = useContext(LocaleContext);
	const [tab, setTab] = useState<"emoji" | "emotes">("emoji");
	const [search, setSearch] = useState("");
	const containerRef = useRef<HTMLDivElement>(null);

	useEffect(() => {
		const onPointerDown = (event: PointerEvent) => {
			const target = event.target as Node;
			if (containerRef.current && !containerRef.current.contains(target) && !anchorRef.current?.contains(target)) {
				onClose();
			}
		};
		const onKeyDown = (event: KeyboardEvent) => event.key === "Escape" && onClose();
		document.addEventListener("pointerdown", onPointerDown);
		document.addEventListener("keydown", onKeyDown);
		return () => {
			document.removeEventListener("pointerdown", onPointerDown);
			document.removeEventListener("keydown", onKeyDown);
		};
	}, [anchorRef, onClose]);

	const query = search.trim().toLowerCase();
	const categories = useMemo(() => EMOJI_CATEGORIES
		.map((category) => ({
			...category,
			emojis: query ? category.emojis.filter((item) => item.keywords.includes(query) || item.emoji === query) : category.emojis,
		}))
		.filter((category) => category.emojis.length > 0), [query]);
	const filteredEmotes = useMemo(() => (query
		? emotes.filter((emote) => emote.code.toLowerCase().includes(query))
		: emotes).slice(0, MAX_EMOTES_SHOWN), [emotes, query]);

	const tabClass = (active: boolean) =>
		`flex-1 rounded px-2 py-1 text-xs font-semibold ${active ? "bg-gray-700 text-white" : "text-gray-400 hover:text-gray-200"}`;

	if (!position) {
		return null;
	}

	return createPortal(
		<div
			ref={containerRef}
			style={position}
			className="fixed z-[90] flex flex-col rounded-md border border-gray-700 bg-gray-900 text-gray-100 shadow-xl"
		>
			<div className="flex gap-1 border-b border-gray-700 p-1">
				<button type="button" className={tabClass(tab === "emoji")} onClick={() => setTab("emoji")}>{locale.chat.emoji_tab_emoji}</button>
				<button type="button" className={tabClass(tab === "emotes")} onClick={() => setTab("emotes")}>{locale.chat.emoji_tab_emotes}</button>
			</div>

			<input
				type="text"
				autoFocus
				value={search}
				onChange={(event) => setSearch(event.target.value)}
				placeholder={locale.chat.emoji_search_placeholder}
				className="m-2 h-8 rounded-md border border-gray-700 bg-gray-800 px-2 text-sm text-gray-100 placeholder:text-gray-500 focus:outline-hidden"
			/>

			<div style={{ colorScheme: "dark" }} className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
				{tab === "emoji" && (
					<>
						{categories.length === 0 && <p className="p-2 text-xs text-gray-400">{locale.chat.emoji_no_results}</p>}
						{categories.map((category) => (
							<div key={category.name}>
								<div className="sticky top-0 bg-gray-900 py-1 text-xs text-gray-400">{category.icon} {category.name}</div>
								<div className="grid grid-cols-8 gap-0.5">
									{category.emojis.map((item) => (
										<button
											type="button"
											key={item.emoji}
											title={item.keywords}
											onClick={() => onPick(item.emoji)}
											className="flex h-8 items-center justify-center rounded text-xl hover:bg-gray-700"
										>
											{item.emoji}
										</button>
									))}
								</div>
							</div>
						))}
					</>
				)}

				{tab === "emotes" && (
					<>
						{emotes.length === 0 && <p className="p-2 text-xs text-gray-400">{locale.chat.emotes_not_configured}</p>}
						{emotes.length > 0 && filteredEmotes.length === 0 && <p className="p-2 text-xs text-gray-400">{locale.chat.emoji_no_results}</p>}
						<div className="grid grid-cols-6 gap-1">
							{filteredEmotes.map((emote) => (
								<button
									type="button"
									key={`${emote.provider}-${emote.code}`}
									title={`${emote.code} (${emote.provider.toUpperCase()})`}
									onClick={() => onPick(emote.code)}
									className="flex h-10 items-center justify-center rounded hover:bg-gray-700"
								>
									<img src={emote.url} alt={emote.code} loading="lazy" className="max-h-8 max-w-10 object-contain" />
								</button>
							))}
						</div>
					</>
				)}
			</div>
		</div>,
		document.body,
	);
};

export default EmojiPicker;
