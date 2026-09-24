// The full Unicode emoji set (emojibase), loaded the first time the picker
// opens so it doesn't weigh on the page load.

export interface EmojiEntry {
	emoji: string;
	label: string;
	// Label and tags, lower case, for searching
	keywords: string;
}

export interface EmojiCategory {
	name: string;
	icon: string;
	emojis: EmojiEntry[];
}

interface CompactEmoji {
	unicode: string;
	label: string;
	group?: number;
	order?: number;
	tags?: string[];
}

// emojibase groups; 2 holds skin tone and hair components, not emojis
const GROUPS: Record<number, { name: string; icon: string }> = {
	0: { name: "Smileys & Emotion", icon: "😀" },
	1: { name: "People & Body", icon: "👋" },
	3: { name: "Animals & Nature", icon: "🐶" },
	4: { name: "Food & Drink", icon: "🍕" },
	5: { name: "Travel & Places", icon: "✈️" },
	6: { name: "Activities", icon: "🎮" },
	7: { name: "Objects", icon: "💡" },
	8: { name: "Symbols", icon: "❤️" },
	9: { name: "Flags", icon: "🏁" },
};

let categoriesPromise: Promise<EmojiCategory[]> | undefined;

export const loadEmojiCategories = (): Promise<EmojiCategory[]> => {
	categoriesPromise ??= import("emojibase-data/en/compact.json").then((module) => {
		const data = (module.default ?? module) as unknown as CompactEmoji[];
		const categories = new Map<number, EmojiCategory>();
		const sorted = [...data].sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
		for (const entry of sorted) {
			const group = entry.group === undefined ? undefined : GROUPS[entry.group];
			if (!group) {
				continue;
			}
			let category = categories.get(entry.group!);
			if (!category) {
				category = { ...group, emojis: [] };
				categories.set(entry.group!, category);
			}
			category.emojis.push({
				emoji: entry.unicode,
				label: entry.label,
				keywords: [entry.label, ...(entry.tags ?? [])].join(" ").toLowerCase(),
			});
		}
		return [...categories.keys()].sort((a, b) => a - b).map((key) => categories.get(key)!);
	});
	return categoriesPromise;
};

// Emojis whose label or tags start with the query words, best matches first
export const searchEmojis = (categories: EmojiCategory[], query: string, limit = 200): EmojiEntry[] => {
	const words = query.toLowerCase().split(/\s+/).filter(Boolean);
	if (words.length === 0) {
		return [];
	}

	const results: { entry: EmojiEntry; score: number }[] = [];
	for (const category of categories) {
		for (const entry of category.emojis) {
			const keywordWords = entry.keywords.split(/[\s:,-]+/);
			let score = 0;
			for (const word of words) {
				if (keywordWords.includes(word)) {
					score += 3;
				} else if (keywordWords.some((keyword) => keyword.startsWith(word))) {
					score += 2;
				} else if (entry.keywords.includes(word)) {
					score += 1;
				} else {
					score = -1;
					break;
				}
			}
			if (score > 0) {
				const label = entry.label.toLowerCase();
				if (label === words.join(" ")) {
					score += 10;
				} else if (label.startsWith(words[0])) {
					score += 2;
				}
				results.push({ entry, score });
			}
		}
	}

	return results.sort((a, b) => b.score - a.score).slice(0, limit).map((result) => result.entry);
};
