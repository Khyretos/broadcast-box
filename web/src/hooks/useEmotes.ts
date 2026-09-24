import { useEffect, useState } from "react";

export interface Emote {
	code: string;
	url: string;
	url2x?: string;
	provider: "twitch" | "7tv" | "bttv" | "ffz";
	animated?: boolean;
}

export type EmoteMap = Map<string, Emote>;

export interface GifSources {
	// Searched when the viewer presses enter: "giphy", "klipy"
	apis: string[];
	// Hosts of the configured Slink instances
	slink: string[];
}

interface EmotesResponse {
	emotes: Emote[];
	gifHosts: string[];
	gifSources: GifSources;
}

const noGifSources: GifSources = { apis: [], slink: [] };

export interface StreamEmotes {
	streamKey: string;
	list: Emote[];
	map: EmoteMap;
	// Hosts whose images are shown inline in chat, "*.example.com" for a
	// domain and its subdomains, "*" for any
	gifHosts: string[];
	// GIF search configured on the server
	gifSources: GifSources;
}

const cache = new Map<string, Promise<EmotesResponse>>();

const loadEmotes = (streamKey: string) => {
	let request = cache.get(streamKey);
	if (!request) {
		request = fetch(`/api/chat/emotes?key=${encodeURIComponent(streamKey)}`)
			.then((response): Promise<Partial<EmotesResponse>> | Partial<EmotesResponse> => response.ok ? response.json() : {})
			.then((body) => ({ emotes: body.emotes ?? [], gifHosts: body.gifHosts ?? [], gifSources: body.gifSources ?? noGifSources }))
			.catch(() => ({ emotes: [], gifHosts: [], gifSources: noGifSources }));
		cache.set(streamKey, request);
	}
	return request;
};

const emptyEmotes = (streamKey: string): StreamEmotes => ({ streamKey, list: [], map: new Map(), gifHosts: [], gifSources: noGifSources });

// Emotes configured for the stream (Twitch, 7TV, BetterTTV, FrankerFaceZ)
export const useEmotes = (streamKey: string): StreamEmotes => {
	const [emotes, setEmotes] = useState<StreamEmotes>(() => emptyEmotes(""));

	useEffect(() => {
		let cancelled = false;
		loadEmotes(streamKey).then((body) => {
			if (!cancelled) {
				setEmotes({
					streamKey,
					list: body.emotes,
					map: new Map(body.emotes.map((emote) => [emote.code, emote])),
					gifHosts: body.gifHosts,
					gifSources: body.gifSources,
				});
			}
		});
		return () => {
			cancelled = true;
		};
	}, [streamKey]);

	return emotes.streamKey === streamKey ? emotes : emptyEmotes(streamKey);
};

const searchCache = new Map<string, Promise<Emote[]>>();

// Searches 7TV, BetterTTV and FrankerFaceZ through the server
export const searchEmotes = (query: string): Promise<Emote[]> => {
	const key = query.trim().toLowerCase();
	let request = searchCache.get(key);
	if (!request) {
		request = fetch(`/api/chat/emotes/search?q=${encodeURIComponent(key)}`)
			.then((response): Promise<{ emotes?: Emote[] }> | { emotes?: Emote[] } => response.ok ? response.json() : {})
			.then((body) => body.emotes ?? [])
			.catch(() => []);
		searchCache.set(key, request);
		// Allow retrying failed or stale searches later
		setTimeout(() => searchCache.delete(key), 5 * 60_000);
	}
	return request;
};

export const providerName = (provider: Emote["provider"]) => ({
	twitch: "Twitch",
	"7tv": "7TV",
	bttv: "BetterTTV",
	ffz: "FrankerFaceZ",
})[provider] ?? provider;

// "gifs.example.com" matches that host, "*.example.com" example.com and its
// subdomains, "*" any host. Same rules as the server.
const matchesHost = (pattern: string, host: string) => {
	if (pattern === "*") {
		return true;
	}
	if (pattern.startsWith("*.")) {
		const domain = pattern.slice(2);
		return host === domain || host.endsWith(`.${domain}`);
	}
	return host === pattern;
};

export const isAllowedGifUrl = (value: string, gifHosts: string[]) => {
	if (gifHosts.length === 0 || !value.startsWith("https://")) {
		return false;
	}
	try {
		const host = new URL(value).hostname.toLowerCase();
		return gifHosts.some((pattern) => matchesHost(pattern.toLowerCase(), host));
	} catch {
		return false;
	}
};

export interface GifResult {
	url: string;
	preview: string;
	width?: number;
	height?: number;
	title?: string;
	provider: "giphy" | "klipy" | "slink";
	// Host the GIF comes from
	source: string;
}

export interface GifSearchResult {
	gifs: GifResult[];
	// Giphy was skipped because the server's hourly Giphy budget is used up
	// API providers skipped because their hourly budget is used up
	limited?: string[];
}

const gifSearchCache = new Map<string, Promise<GifSearchResult>>();

// Searches the Slink instances, and Giphy when includeGiphy is set. Giphy
// allows few requests per hour, so it is only searched on enter.
// Brand names of the GIF search APIs, shown for attribution
export const gifApiName = (api: string) => ({ giphy: "GIPHY", klipy: "KLIPY" })[api] ?? api;

export const searchGifs = (query: string, includeApis: boolean): Promise<GifSearchResult> => {
	const key = `${includeApis ? "apis" : "slink"}:${query.trim().toLowerCase()}`;
	let request = gifSearchCache.get(key);
	if (!request) {
		request = fetch(`/api/chat/gifs/search?q=${encodeURIComponent(query.trim())}${includeApis ? "&apis" : ""}`)
			.then((response): Promise<Partial<GifSearchResult>> | Partial<GifSearchResult> => response.ok ? response.json() : {})
			.then((body) => ({ gifs: body.gifs ?? [], limited: body.limited }))
			.catch(() => ({ gifs: [] }));
		gifSearchCache.set(key, request);
		setTimeout(() => gifSearchCache.delete(key), 60_000);
	}
	return request;
};
