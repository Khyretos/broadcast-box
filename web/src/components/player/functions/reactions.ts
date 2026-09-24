import { Emote } from "../../../hooks/useEmotes";

// A reaction is any emoji, or an emote hosted by one of the emote providers
export type Reaction =
	| { emoji: string; emote?: undefined }
	| { emoji?: undefined; emote: Pick<Emote, "code" | "url"> };

export const DEFAULT_REACTION: Reaction = { emoji: "❤️" };

// Aggregated reactions sent by the server every 250ms
export interface ReactionsMessage {
	type: "reactions";
	counts: Record<string, number>;
	emotes?: { code: string; url: string; count: number }[];
}

const STORAGE_KEY = "bb-selected-reaction";

export const loadSelectedReaction = (): Reaction => {
	try {
		const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "null") as Reaction | null;
		if (stored?.emoji || (stored?.emote?.code && stored.emote.url)) {
			return stored;
		}
	} catch {
		// Fall back to the default
	}
	return DEFAULT_REACTION;
};

export const saveSelectedReaction = (reaction: Reaction) => {
	try {
		localStorage.setItem(STORAGE_KEY, JSON.stringify(reaction));
	} catch {
		// Not persisted, still selected for this page
	}
};
