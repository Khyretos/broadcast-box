// Emojis viewers can react with. Must match allowedReactions in
// internal/webrtc/sessions/session/reactions.go
export const REACTION_EMOJIS = ["❤️", "😂", "🔥", "👏", "😮", "🎉", "👍", "😢", "💯", "🙏"] as const;

export type ReactionEmoji = typeof REACTION_EMOJIS[number];

export const DEFAULT_REACTION: ReactionEmoji = "❤️";

// Aggregated reactions sent by the server every 250ms
export interface ReactionsMessage {
	type: "reactions";
	counts: Record<string, number>;
}
