import { useEffect, useState } from "react";

export interface Emote {
	code: string;
	url: string;
	url2x?: string;
	provider: "7tv" | "bttv" | "ffz";
	animated?: boolean;
}

export type EmoteMap = Map<string, Emote>;

const cache = new Map<string, Promise<Emote[]>>();

const loadEmotes = (streamKey: string) => {
	let request = cache.get(streamKey);
	if (!request) {
		request = fetch(`/api/chat/emotes?key=${encodeURIComponent(streamKey)}`)
			.then((response) => response.ok ? response.json() as Promise<{ emotes: Emote[] }> : { emotes: [] })
			.then((body) => body.emotes ?? [])
			.catch(() => []);
		cache.set(streamKey, request);
	}
	return request;
};

// 7TV, BetterTTV and FrankerFaceZ emotes configured for the stream
export const useEmotes = (streamKey: string) => {
	const [emotes, setEmotes] = useState<{ streamKey: string; list: Emote[]; map: EmoteMap }>({ streamKey: "", list: [], map: new Map() });

	useEffect(() => {
		let cancelled = false;
		loadEmotes(streamKey).then((list) => {
			if (!cancelled) {
				setEmotes({ streamKey, list, map: new Map(list.map((emote) => [emote.code, emote])) });
			}
		});
		return () => {
			cancelled = true;
		};
	}, [streamKey]);

	return emotes.streamKey === streamKey ? emotes : { streamKey, list: [] as Emote[], map: new Map() as EmoteMap };
};
