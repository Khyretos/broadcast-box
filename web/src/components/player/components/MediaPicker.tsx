import { RefObject, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { LocaleContext } from "../../../providers/LocaleProvider";
import { EmojiCategory, loadEmojiCategories, searchEmojis } from "../functions/emojiData";
import { Emote, GifSearchResult, StreamEmotes, gifApiName, isAllowedGifUrl, providerName, searchEmotes, searchGifs } from "../../../hooks/useEmotes";
import { fuzzyFilter, fuzzyScore } from "../functions/fuzzy";
import { RecentItem, getRecent, onRecentChanged } from "../functions/recentItems";

type Tab = "emoji" | "emotes" | "gifs";

interface MediaPickerProps {
	// The picker opens above this element
	anchorRef: RefObject<HTMLElement | null>;
	// Reactions can be emojis and emotes, chat can also insert GIFs
	mode: "chat" | "reaction";
	emotes: StreamEmotes;
	onPick(item: RecentItem): void;
	onClose(): void;
}

const MAX_EMOTES_SHOWN = 300;
// "GIPHY & KLIPY"
const gifApiNames = (apis: string[]) => {
	const names = apis.map(gifApiName);
	return names.length > 1 ? `${names.slice(0, -1).join(", ")} & ${names[names.length - 1]}` : names.join("");
};

const SEARCH_DELAY_MS = 300;
// Rendering all ~1900 emojis at once is slow, categories render in steps
const EMOJIS_PER_CATEGORY_STEP = 120;

const useRecentItems = () => {
	const [version, setVersion] = useState(0);
	useEffect(() => onRecentChanged(() => setVersion((current) => current + 1)), []);
	return useMemo(() => ({
		emojis: getRecent("emoji"),
		emotes: getRecent("emote"),
		gifs: getRecent("gif"),
	// eslint-disable-next-line react-hooks/exhaustive-deps
	}), [version]);
};

const SectionTitle = (props: { children: React.ReactNode }) => (
	<div className="sticky top-0 z-10 bg-gray-900 py-1 text-xs text-gray-400">{props.children}</div>
);

const EmoteButton = (props: { emote: Emote; onPick(emote: Emote): void }) => (
	<button
		type="button"
		title={`${props.emote.code} (${providerName(props.emote.provider)})`}
		onClick={() => props.onPick(props.emote)}
		className="flex h-10 items-center justify-center rounded hover:bg-gray-700"
	>
		<img src={props.emote.url} alt={props.emote.code} loading="lazy" className="max-h-8 max-w-10 object-contain" />
	</button>
);

const MediaPicker = (props: MediaPickerProps) => {
	const { anchorRef, mode, emotes, onPick, onClose } = props;
	const { locale } = useContext(LocaleContext);
	const [tab, setTab] = useState<Tab>("emoji");
	const [search, setSearch] = useState("");
	const [remote, setRemote] = useState<{ query: string; emotes: Emote[] }>({ query: "", emotes: [] });
	const [gifResults, setGifResults] = useState<GifSearchResult & { query: string; includesApis: boolean }>();
	// Giphy and KLIPY allow few requests per hour: only searched for the query
	// the viewer pressed enter on
	const [apiQuery, setApiQuery] = useState<string>();
	const [gifLink, setGifLink] = useState("");
	const [gifError, setGifError] = useState(false);
	const [position, setPosition] = useState<{ left: number; bottom: number; width: number; height: number }>();
	const containerRef = useRef<HTMLDivElement>(null);
	const recent = useRecentItems();
	const query = search.trim();
	const [emojiCategories, setEmojiCategories] = useState<EmojiCategory[]>();
	const [expandedCategories, setExpandedCategories] = useState<Record<string, boolean>>({});

	useEffect(() => {
		let cancelled = false;
		loadEmojiCategories().then((categories) => !cancelled && setEmojiCategories(categories));
		return () => {
			cancelled = true;
		};
	}, []);

	// Rendered in a portal with fixed positioning, so the chat panel's
	// overflow can't clip it
	useLayoutEffect(() => {
		const update = () => {
			const rect = anchorRef.current?.getBoundingClientRect();
			if (!rect) {
				return;
			}
			const width = Math.min(400, window.innerWidth - 16);
			setPosition({
				left: Math.max(8, Math.min(rect.left, window.innerWidth - width - 8)),
				bottom: window.innerHeight - rect.top + 8,
				width,
				height: Math.max(220, Math.min(420, rect.top - 16)),
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

	// Search the emote providers while typing in the emotes tab
	useEffect(() => {
		if (tab !== "emotes" || query.length < 2) {
			return;
		}
		let cancelled = false;
		const timeout = setTimeout(() => {
			searchEmotes(query).then((found) => !cancelled && setRemote({ query, emotes: found }));
		}, SEARCH_DELAY_MS);
		return () => {
			cancelled = true;
			clearTimeout(timeout);
		};
	}, [query, tab]);

	const gifSources = emotes.gifSources;
	const hasGifApis = gifSources.apis.length > 0;
	const hasGifSearch = hasGifApis || gifSources.slink.length > 0;
	const includeApis = hasGifApis && query !== "" && apiQuery === query;

	// Slink is searched while typing (newest images when empty), Giphy and KLIPY on enter
	useEffect(() => {
		if (tab !== "gifs" || (!includeApis && gifSources.slink.length === 0)) {
			return;
		}
		let cancelled = false;
		const timeout = setTimeout(() => {
			searchGifs(query, includeApis).then((result) => !cancelled && setGifResults({ ...result, query, includesApis: includeApis }));
		}, query && !includeApis ? SEARCH_DELAY_MS : 0);
		return () => {
			cancelled = true;
			clearTimeout(timeout);
		};
	}, [gifSources.slink.length, includeApis, query, tab]);

	const currentGifResults = gifResults?.query === query && gifResults.includesApis === includeApis ? gifResults : undefined;
	const isSearchingGifs = tab === "gifs" && (includeApis || gifSources.slink.length > 0) && !currentGifResults;
	const resultApis = gifSources.apis.filter((api) => currentGifResults?.gifs.some((gif) => gif.provider === api));

	const recentGifs = useMemo(() => {
		const allowed = recent.gifs.filter((item) => isAllowedGifUrl(item.url, emotes.gifHosts));
		return query ? fuzzyFilter(allowed.filter((item) => item.title), query, (item) => item.title ?? "", 30) : allowed;
	}, [emotes.gifHosts, query, recent.gifs]);

	const emojiResults = useMemo(() => searchEmojis(emojiCategories ?? [], query), [emojiCategories, query]);

	const localEmotes = useMemo(() => {
		const byUrl = new Map<string, Emote>();
		[...recent.emotes.map((item) => item.emote), ...emotes.list].forEach((emote) => byUrl.set(emote.url, emote));
		return [...byUrl.values()];
	}, [emotes.list, recent.emotes]);
	const localEmoteResults = useMemo(
		() => fuzzyFilter(localEmotes, query, (emote) => emote.code, MAX_EMOTES_SHOWN),
		[localEmotes, query],
	);
	const remoteResults = useMemo(() => {
		if (remote.query !== query) {
			return undefined;
		}
		const localUrls = new Set(localEmoteResults.map((emote) => emote.url));
		return remote.emotes
			.filter((emote) => !localUrls.has(emote.url))
			.map((emote) => ({ emote, score: fuzzyScore(query, emote.code) }))
			.sort((a, b) => b.score - a.score)
			.map((entry) => entry.emote);
	}, [localEmoteResults, query, remote]);

	const pickEmote = (emote: Emote) => onPick({ kind: "emote", emote });

	const addGifLink = () => {
		const url = gifLink.trim();
		if (!isAllowedGifUrl(url, emotes.gifHosts)) {
			setGifError(true);
			return;
		}
		setGifLink("");
		setGifError(false);
		onPick({ kind: "gif", url });
	};

	const tabs: Tab[] = mode === "chat" ? ["emoji", "emotes", "gifs"] : ["emoji", "emotes"];
	const tabLabel = (value: Tab) => ({
		emoji: locale.chat.emoji_tab_emoji,
		emotes: locale.chat.emoji_tab_emotes,
		gifs: locale.chat.emoji_tab_gifs,
	})[value];

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
				{tabs.map((value) => (
					<button
						type="button"
						key={value}
						onClick={() => setTab(value)}
						className={`flex-1 rounded px-2 py-1 text-xs font-semibold ${tab === value ? "bg-gray-700 text-white" : "text-gray-400 hover:text-gray-200"}`}
					>
						{tabLabel(value)}
					</button>
				))}
			</div>

			{(tab !== "gifs" || hasGifSearch) && (
				<input
					type="text"
					autoFocus
					value={search}
					onChange={(event) => setSearch(event.target.value)}
					onKeyDown={(event) => {
						if (event.key === "Enter" && tab === "gifs" && hasGifApis && query) {
							event.preventDefault();
							setApiQuery(query);
						}
					}}
					placeholder={({
						emoji: locale.chat.emoji_search_placeholder,
						emotes: locale.chat.emote_search_placeholder,
						gifs: locale.chat.gif_search_placeholder,
					})[tab]}
					className="m-2 h-8 rounded-md border border-gray-700 bg-gray-800 px-2 text-sm text-gray-100 placeholder:text-gray-500 focus:outline-hidden"
				/>
			)}
			{tab === "gifs" && hasGifApis && query && !includeApis && (
				<p className="-mt-1 mb-1 px-2 text-[11px] text-gray-400">{locale.chat.gif_press_enter_search.replace("{providers}", gifApiNames(gifSources.apis))}</p>
			)}

			<div style={{ colorScheme: "dark" }} className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
				{tab === "emoji" && (
					<>
						{!query && recent.emojis.length > 0 && (
							<>
								<SectionTitle>🕘 {locale.chat.picker_frequently_used}</SectionTitle>
								<div className="grid grid-cols-8 gap-0.5">
									{recent.emojis.slice(0, 16).map((item) => (
										<button type="button" key={item.emoji} onClick={() => onPick(item)} className="flex h-8 items-center justify-center rounded text-xl hover:bg-gray-700">
											{item.emoji}
										</button>
									))}
								</div>
							</>
						)}

						{!emojiCategories && <p className="animate-pulse p-2 text-xs text-gray-400">{locale.chat.emotes_searching}</p>}
						{emojiCategories && query ? (
							<>
								{emojiResults.length === 0 && <p className="p-2 text-xs text-gray-400">{locale.chat.emoji_no_results}</p>}
								<div className="grid grid-cols-8 gap-0.5 pt-1">
									{emojiResults.map((item) => (
										<button type="button" key={item.emoji} title={item.label} onClick={() => onPick({ kind: "emoji", emoji: item.emoji })} className="flex h-8 items-center justify-center rounded text-xl hover:bg-gray-700">
											{item.emoji}
										</button>
									))}
								</div>
							</>
						) : emojiCategories?.map((category) => {
							const isExpanded = expandedCategories[category.name];
							const shown = isExpanded ? category.emojis : category.emojis.slice(0, EMOJIS_PER_CATEGORY_STEP);
							return (
								<div key={category.name}>
									<SectionTitle>{category.icon} {category.name}</SectionTitle>
									<div className="grid grid-cols-8 gap-0.5">
										{shown.map((item) => (
											<button type="button" key={item.emoji} title={item.label} onClick={() => onPick({ kind: "emoji", emoji: item.emoji })} className="flex h-8 items-center justify-center rounded text-xl hover:bg-gray-700">
												{item.emoji}
											</button>
										))}
									</div>
									{!isExpanded && category.emojis.length > shown.length && (
										<button
											type="button"
											onClick={() => setExpandedCategories((current) => ({ ...current, [category.name]: true }))}
											className="mt-1 w-full rounded py-1 text-xs text-gray-400 hover:bg-gray-800 hover:text-gray-200"
										>
											+{category.emojis.length - shown.length}
										</button>
									)}
								</div>
							);
						})}
					</>
				)}

				{tab === "emotes" && (
					<>
						{!query && recent.emotes.length > 0 && (
							<>
								<SectionTitle>🕘 {locale.chat.picker_frequently_used}</SectionTitle>
								<div className="grid grid-cols-6 gap-1">
									{recent.emotes.slice(0, 18).map((item) => <EmoteButton key={item.emote.url} emote={item.emote} onPick={pickEmote} />)}
								</div>
							</>
						)}

						{(query ? localEmoteResults.length > 0 : emotes.list.length > 0) && (
							<SectionTitle>{query ? locale.chat.emotes_matching : locale.chat.emotes_channel}</SectionTitle>
						)}
						{!query && emotes.list.length === 0 && <p className="p-2 text-xs text-gray-400">{locale.chat.emotes_search_hint}</p>}
						<div className="grid grid-cols-6 gap-1">
							{(query ? localEmoteResults : emotes.list.slice(0, MAX_EMOTES_SHOWN)).map((emote) => (
								<EmoteButton key={`${emote.provider}-${emote.url}`} emote={emote} onPick={pickEmote} />
							))}
						</div>

						{query.length >= 2 && (
							<>
								<SectionTitle>{locale.chat.emotes_search_results}</SectionTitle>
								{remoteResults === undefined && <p className="animate-pulse p-2 text-xs text-gray-400">{locale.chat.emotes_searching}</p>}
								{remoteResults?.length === 0 && localEmoteResults.length === 0 && <p className="p-2 text-xs text-gray-400">{locale.chat.emoji_no_results}</p>}
								<div className="grid grid-cols-6 gap-1">
									{remoteResults?.map((emote) => <EmoteButton key={`${emote.provider}-${emote.url}`} emote={emote} onPick={pickEmote} />)}
								</div>
							</>
						)}
					</>
				)}

				{tab === "gifs" && (
					<>
						{emotes.gifHosts.length === 0 && !hasGifSearch && (
							<p className="p-2 text-xs text-gray-400">{locale.chat.gifs_disabled}</p>
						)}

						{recentGifs.length > 0 && (
							<>
								<SectionTitle>🕘 {locale.chat.picker_frequently_used}</SectionTitle>
								<div className="grid grid-cols-3 gap-1">
									{recentGifs.map((item) => (
										<button type="button" key={item.url} title={item.title} onClick={() => onPick(item)} className="flex h-24 items-center justify-center overflow-hidden rounded bg-gray-800 hover:ring-2 hover:ring-blue-500">
											<img src={item.preview ?? item.url} alt={item.title ?? ""} loading="lazy" className="max-h-24 max-w-full object-contain" />
										</button>
									))}
								</div>
							</>
						)}

						{(query ? isSearchingGifs || currentGifResults : gifSources.slink.length > 0) && (
							<SectionTitle>
								{query ? locale.chat.gifs_search_results : locale.chat.gifs_latest.replace("{source}", gifSources.slink.join(", "))}
							</SectionTitle>
						)}
						{isSearchingGifs && <p className="animate-pulse p-2 text-xs text-gray-400">{locale.chat.emotes_searching}</p>}
						{currentGifResults?.limited && currentGifResults.limited.length > 0 && (
							<p className="p-2 text-xs text-yellow-300">{locale.chat.gif_search_limited.replace("{providers}", gifApiNames(currentGifResults.limited))}</p>
						)}
						{currentGifResults && currentGifResults.gifs.length === 0 && recentGifs.length === 0 && (
							<p className="p-2 text-xs text-gray-400">{locale.chat.emoji_no_results}</p>
						)}
						{currentGifResults && currentGifResults.gifs.length > 0 && (
							<div className="columns-2 gap-1">
								{currentGifResults.gifs.map((gif) => (
									<button
										type="button"
										key={gif.url}
										title={`${gif.title ?? ""} (${gif.source})`}
										onClick={() => onPick({ kind: "gif", url: gif.url, preview: gif.preview, title: gif.title })}
										className="mb-1 block w-full overflow-hidden rounded bg-gray-800 hover:ring-2 hover:ring-blue-500"
									>
										<img
											src={gif.preview}
											alt={gif.title ?? ""}
											loading="lazy"
											className="block w-full"
											style={gif.width && gif.height ? { aspectRatio: `${gif.width} / ${gif.height}` } : undefined}
										/>
									</button>
								))}
							</div>
						)}
						{resultApis.length > 0 && (
							<p className="mt-1 text-right text-[10px] text-gray-500">{locale.chat.gifs_powered_by.replace("{providers}", resultApis.map(gifApiName).join(" · "))}</p>
						)}

						{emotes.gifHosts.length > 0 && (
							<form
								className="mt-2 flex gap-1"
								onSubmit={(event) => {
									// React bubbles portal events to the chat form, don't submit that too
									event.preventDefault();
									event.stopPropagation();
									addGifLink();
								}}
							>
								<input
									type="url"
									autoFocus={!hasGifSearch}
									value={gifLink}
									onChange={(event) => {
										setGifLink(event.target.value);
										setGifError(false);
									}}
									placeholder={locale.chat.gif_link_placeholder}
									className="h-8 min-w-0 flex-1 rounded-md border border-gray-700 bg-gray-800 px-2 text-sm text-gray-100 placeholder:text-gray-500 focus:outline-hidden"
								/>
								<button type="submit" className="rounded-md bg-blue-600 px-3 text-xs font-semibold hover:bg-blue-500">
									{locale.chat.gif_add}
								</button>
							</form>
						)}
						{gifError && <p className="mt-1 text-xs text-red-300">{locale.chat.gif_host_not_allowed}</p>}
					</>
				)}
			</div>
		</div>,
		document.body,
	);
};

export default MediaPicker;
