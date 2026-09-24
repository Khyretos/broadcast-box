// Scores how well a query fuzzy matches a text: the query's characters must
// appear in order. Higher is better, -1 means no match. Exact and prefix
// matches, consecutive characters and word starts score higher.
export const fuzzyScore = (query: string, text: string): number => {
	const q = query.toLowerCase();
	const t = text.toLowerCase();
	if (q.length === 0) {
		return 0;
	}
	if (t === q) {
		return 1000;
	}

	let score = t.startsWith(q) ? 500 : t.includes(q) ? 250 : 0;
	let textIndex = 0;
	let previousMatch = -2;
	for (const character of q) {
		const found = t.indexOf(character, textIndex);
		if (found < 0) {
			return -1;
		}
		if (found === previousMatch + 1) {
			score += 5;
		}
		const isWordStart = found === 0 || /[^a-z0-9]/.test(t[found - 1]) || (text[found] !== t[found] && text[found - 1] === t[found - 1]);
		if (isWordStart) {
			score += 3;
		}
		previousMatch = found;
		textIndex = found + 1;
	}

	// Prefer shorter texts for the same match
	return score - Math.min(t.length - q.length, 50) * 0.1;
};

export const fuzzyFilter = <T,>(items: T[], query: string, text: (item: T) => string, limit = 300): T[] => {
	if (!query.trim()) {
		return items.slice(0, limit);
	}
	return items
		.map((item) => ({ item, score: fuzzyScore(query.trim(), text(item)) }))
		.filter((entry) => entry.score >= 0)
		.sort((a, b) => b.score - a.score)
		.slice(0, limit)
		.map((entry) => entry.item);
};
