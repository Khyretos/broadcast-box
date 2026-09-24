package environment

const (
	// SERVER
	appEnv                     = "APP_ENV"
	HTTPAddress                = "HTTP_ADDRESS"
	HTTPSRedirectPort          = "HTTPS_REDIRECT_PORT"
	HTTPEnableRedirect         = "ENABLE_HTTP_REDIRECT"
	NetworkTestOnStart         = "NETWORK_TEST_ON_START"
	IncludePublicIPInNAT1To1IP = "INCLUDE_PUBLIC_IP_IN_NAT_1_TO_1_IP"
	DisableStatus              = "DISABLE_STATUS"
	EnableProfiling            = "ENABLE_PROFILING"

	// SSL
	SSLKey  = "SSL_KEY"
	SSLCert = "SSL_CERT"

	// AUTHORIZATION
	StreamProfilePath   = "STREAM_PROFILE_PATH"
	StreamProfilePolicy = "STREAM_PROFILE_POLICY"
	WebhookURL          = "WEBHOOK_URL"

	// FRONTEND
	FrontendDisabled   = "DISABLE_FRONTEND"
	frontendPath       = "FRONTEND_PATH"
	FrontendAdminToken = "FRONTEND_ADMIN_TOKEN"

	// CHAT
	ChatMaxHistory      = "CHAT_MAX_HISTORY"
	ChatDefaultTTL      = "CHAT_DEFAULT_TTL"
	ChatCleanupInterval = "CHAT_CLEANUP_INTERVAL"
	ChatEmoteProviders  = "CHAT_EMOTE_PROVIDERS"
	ChatEmotesTwitchIDs = "CHAT_EMOTES_TWITCH_IDS"
	ChatGIFHosts        = "CHAT_GIF_HOSTS"
	GiphyAPIKey         = "GIPHY_API_KEY"
	TenorAPIKey         = "TENOR_API_KEY"
	GIFContentRating    = "GIF_CONTENT_RATING"
	TwitchClientID      = "TWITCH_CLIENT_ID"
	TwitchClientSecret  = "TWITCH_CLIENT_SECRET"

	// NOTIFICATIONS
	DiscordWebhookURL        = "DISCORD_WEBHOOK_URL"
	PublicURL                = "PUBLIC_URL"
	NotifyOfflineGracePeriod = "NOTIFY_OFFLINE_GRACE_PERIOD"
	NotifyStreamKeys         = "NOTIFY_STREAM_KEYS"

	// CLIPS
	ClipStoragePath    = "CLIP_STORAGE_PATH"
	ClipS3Endpoint     = "CLIP_S3_ENDPOINT"
	ClipS3Bucket       = "CLIP_S3_BUCKET"
	ClipS3AccessKey    = "CLIP_S3_ACCESS_KEY"
	ClipS3SecretKey    = "CLIP_S3_SECRET_KEY"
	ClipS3Region       = "CLIP_S3_REGION"
	ClipS3Prefix       = "CLIP_S3_PREFIX"
	ClipS3UseSSL       = "CLIP_S3_USE_SSL"
	ClipBufferDuration = "CLIP_BUFFER_DURATION"
	ClipMaxDuration    = "CLIP_MAX_DURATION"
	ClipDraftPath      = "CLIP_DRAFT_PATH"
	ClipMaxDrafts      = "CLIP_MAX_DRAFTS"

	// SOCIAL STREAM NINJA
	SSNSessionID        = "SSN_SESSION_ID"
	SSNStreamKeys       = "SSN_STREAM_KEYS"
	SSNStreamKeysLegacy = "WATCH_STREAM_KEY"
	SSNVerbose          = "SSN_VERBOSE"

	// WEBRTC
	IncludeLoopbackCandidate = "INCLUDE_LOOPBACK_CANDIDATE"
	NetworkTypes             = "NETWORK_TYPES"
	TCPMuxForce              = "TCP_MUX_FORCE"
	TCPMuxAddress            = "TCP_MUX_ADDRESS"
	InterfaceFilter          = "INTERFACE_FILTER"
	UDPMuxPort               = "UDP_MUX_PORT"
	UDPMuxPortWHIP           = "UDP_MUX_PORT_WHIP"
	UDPMuxPortWHEP           = "UDP_MUX_PORT_WHEP"
	UDPMuxReadBufferSize     = "UDP_MUX_READ_BUFFER_SIZE"
	MaxViewersPerStream      = "MAX_VIEWERS_PER_STREAM"
	NAT1To1IP                = "NAT_1_TO_1_IP"
	NATICECandidateType      = "NAT_ICE_CANDIDATE_TYPE"

	// STUN
	STUNServers = "STUN_SERVERS"

	// PEERCONNECTION
	AppendCandidate = "APPEND_CANDIDATE"

	// DEBUGGING
	DebugIncomingAPIRequest = "DEBUG_INCOMING_API_REQUEST"
	DebugPrintAnswer        = "DEBUG_PRINT_ANSWER"
	DebugPrintOffer         = "DEBUG_PRINT_OFFER"
	DebugPrintSSEMessages   = "DEBUG_PRINT_SSE_MESSAGES"

	// LOGGING
	loggingEnabled          = "LOGGING_ENABLED"
	loggingLevel            = "LOGGING_LEVEL"
	loggingDirectory        = "LOGGING_DIRECTORY"
	loggingSingleFile       = "LOGGING_SINGLEFILE"
	loggingNewFileOnStartup = "LOGGING_NEW_FILE_ON_STARTUP"
	LoggingAPIEnabled       = "LOGGING_API_ENABLED"
	LoggingAPIKey           = "LOGGING_API_KEY"
)
