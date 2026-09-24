import { Emote } from "../../../hooks/useEmotes";

// The emojis, emotes and GIFs a viewer uses most, kept in their browser.
// Capped so it never grows without bound.

export type RecentItem =
	| { kind: "emoji"; emoji: string }
	| { kind: "emote"; emote: Emote }
	| { kind: "gif"; url: string };

interface StoredItem {
	item: RecentItem;
	count: number;
	lastUsed: number;
}

const STORAGE_KEY = "bb-recent-items-v1";
const MAX_ITEMS = 50;
const CHANGED_EVENT = "bb-recent-items-changed";

const itemKey = (item: RecentItem) => {
	switch (item.kind) {
		case "emoji":
			return `emoji:${item.emoji}`;
		case "emote":
			return `emote:${item.emote.url}`;
		case "gif":
			return `gif:${item.url}`;
	}
};

const load = (): StoredItem[] => {
	try {
		const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "[]") as StoredItem[];
		return Array.isArray(parsed) ? parsed.filter((entry) => entry?.item?.kind) : [];
	} catch {
		return [];
	}
};

// Most used first, the most recent first among equals
const sortItems = (items: StoredItem[]) => items.sort((a, b) => b.count - a.count || b.lastUsed - a.lastUsed);

export const recordUse = (item: RecentItem) => {
	const items = load();
	const key = itemKey(item);
	const existing = items.find((entry) => itemKey(entry.item) === key);
	if (existing) {
		existing.count += 1;
		existing.lastUsed = Date.now();
		existing.item = item;
	} else {
		// A new item must be able to replace the least used one
		sortItems(items);
		if (items.length >= MAX_ITEMS) {
			items.length = MAX_ITEMS - 1;
		}
		items.push({ item, count: 1, lastUsed: Date.now() });
	}

	try {
		localStorage.setItem(STORAGE_KEY, JSON.stringify(sortItems(items).slice(0, MAX_ITEMS)));
		window.dispatchEvent(new Event(CHANGED_EVENT));
	} catch {
		// Storage full or disabled, recent items are only a convenience
	}
};

export const getRecent = <K extends RecentItem["kind"]>(kind: K): Extract<RecentItem, { kind: K }>[] =>
	sortItems(load())
		.map((entry) => entry.item)
		.filter((item): item is Extract<RecentItem, { kind: K }> => item.kind === kind);

export const onRecentChanged = (callback: () => void) => {
	window.addEventListener(CHANGED_EVENT, callback);
	return () => window.removeEventListener(CHANGED_EVENT, callback);
};
