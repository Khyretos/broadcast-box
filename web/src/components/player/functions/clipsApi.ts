import { getAdminAuthorizationHeader, ADMIN_TOKEN_STORAGE_KEY } from "../../admin/adminAuth";

export interface ClipsConfig {
	enabled: boolean;
	bufferSeconds?: number;
	maxClipSeconds?: number;
}

export interface Clip {
	id: string;
	streamKey: string;
	title: string;
	createdAt: string;
	durationSeconds: number;
	sizeBytes: number;
	videoCodec?: string;
	width?: number;
	height?: number;
}

export interface ClipDraft {
	id: string;
	streamKey: string;
	durationSeconds: number;
	maxClipSeconds: number;
}

export type ClipErrorKind = "nothing_recorded" | "rate_limited" | "draft_expired" | "generic";

export class ClipError extends Error {
	constructor(public kind: ClipErrorKind, message: string) {
		super(message);
	}
}

const CLIPS_UPDATED_EVENT = "bb-clips-updated";

let configPromise: Promise<ClipsConfig> | undefined;

export const getClipsConfig = (): Promise<ClipsConfig> => {
	configPromise ??= fetch(`/api/clips/config`)
		.then((response) => response.ok ? response.json() as Promise<ClipsConfig> : { enabled: false })
		.catch(() => ({ enabled: false }));
	return configPromise;
};

const toClipError = async (response: Response): Promise<ClipError> => {
	const message = (await response.text().catch(() => "")).trim();
	if (response.status === 429) {
		return new ClipError("rate_limited", message);
	}
	if (response.status === 404) {
		return new ClipError("draft_expired", message);
	}
	if (response.status === 400 && message.includes("nothing has been recorded")) {
		return new ClipError("nothing_recorded", message);
	}
	return new ClipError("generic", message);
};

export const createClipDraft = async (streamKey: string): Promise<ClipDraft> => {
	const response = await fetch(`/api/clips/drafts?key=${encodeURIComponent(streamKey)}`, { method: "POST" });
	if (!response.ok) {
		throw await toClipError(response);
	}
	return response.json();
};

export const clipDraftVideoUrl = (draftId: string) => `/api/clips/drafts/${encodeURIComponent(draftId)}`;

export const publishClip = async (draftId: string, start: number, end: number, title: string): Promise<Clip> => {
	const response = await fetch(`/api/clips/drafts/${encodeURIComponent(draftId)}/publish`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ start, end, title }),
	});
	if (!response.ok) {
		throw await toClipError(response);
	}

	const clip = await response.json() as Clip;
	window.dispatchEvent(new CustomEvent(CLIPS_UPDATED_EVENT, { detail: clip.streamKey }));
	return clip;
};

export const listClips = async (streamKey: string): Promise<Clip[]> => {
	const response = await fetch(`/api/clips?key=${encodeURIComponent(streamKey)}`);
	if (!response.ok) {
		throw await toClipError(response);
	}
	return response.json();
};

export const clipVideoUrl = (clip: Clip, download = false) =>
	`/api/clips/${encodeURIComponent(clip.streamKey)}/${encodeURIComponent(clip.id)}.mkv${download ? "?download" : ""}`;

export const canDeleteClips = () => !!localStorage.getItem(ADMIN_TOKEN_STORAGE_KEY);

export const deleteClip = async (clip: Clip): Promise<void> => {
	const response = await fetch(`/api/clips/${encodeURIComponent(clip.streamKey)}/${encodeURIComponent(clip.id)}`, {
		method: "DELETE",
		headers: { Authorization: getAdminAuthorizationHeader() },
	});
	if (!response.ok) {
		throw await toClipError(response);
	}
	window.dispatchEvent(new CustomEvent(CLIPS_UPDATED_EVENT, { detail: clip.streamKey }));
};

export const onClipsUpdated = (streamKey: string, callback: () => void) => {
	const handler = (event: Event) => {
		if ((event as CustomEvent<string>).detail === streamKey) {
			callback();
		}
	};
	window.addEventListener(CLIPS_UPDATED_EVENT, handler);
	return () => window.removeEventListener(CLIPS_UPDATED_EVENT, handler);
};

export const formatClipTime = (seconds: number) => {
	const total = Math.max(0, seconds);
	const minutes = Math.floor(total / 60);
	const rest = total - minutes * 60;
	return `${minutes}:${rest < 10 ? "0" : ""}${rest.toFixed(1)}`;
};

// Title used when the viewer leaves it empty: the local date and time
export const defaultClipTitle = (date = new Date()) =>
	date.toLocaleString([], { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
