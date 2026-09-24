import { useEffect, useState } from "react";

export interface Emote {
	code: string;
	url: string;
	url2x?: string;
	provider: "twitch" | "7tv" | "bttv" | "ffz";
	animated?: boolean;
}

export type EmoteMap = Map<string, Emote>;

interface EmotesResponse {
	emotes: Emote[];
	gifHosts: string[];
}

export interface StreamEmotes {
	streamKey: string;
	list: Emote[];
	map: EmoteMap;
	// Hosts whose images are shown inline in chat, "*" for any
	gifHosts: string[];
}

const cache = new Map<string, Promise<EmotesResponse>>();

const loadEmotes = (streamKey: string) => {
	let request = cache.get(streamKey);
	if (!request) {
		request = fetch(`/api/chat/emotes?key=${encodeURIComponent(streamKey)}`)
			.then((response): Promise<Partial<EmotesResponse>> | Partial<EmotesResponse> => response.ok ? response.json() : {})
			.then((body) => ({ emotes: body.emotes ?? [], gifHosts: body.gifHosts ?? [] }))
			.catch(() => ({ emotes: [], gifHosts: [] }));
		cache.set(streamKey, request);
	}
	return request;
};

const emptyEmotes = (streamKey: string): StreamEmotes => ({ streamKey, list: [], map: new Map(), gifHosts: [] });

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

export const isAllowedGifUrl = (value: string, gifHosts: string[]) => {
	if (gifHosts.length === 0 || !value.startsWith("https://")) {
		return false;
	}
	try {
		const url = new URL(value);
		return gifHosts.includes("*") || gifHosts.includes(url.hostname.toLowerCase());
	} catch {
		return false;
	}
};
