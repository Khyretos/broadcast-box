import { localeInterface } from "./localeInterface"

const locale_da: localeInterface = {
  locale: "da",
  frontpage: {
    welcome: "Velkommen til Broadcast Box",
    welcome_subtitle: "Broadcast Box er et værktøj, der gør det muligt at streame video i høj kvalitet i realtid ved hjælp af de nyeste video-codecs og WebRTC-teknologi.",

    toggle_watch: "Jeg vil se",
    toggle_stream: "Jeg vil streame",

    stream_input_label: "Stream-nøgle",
    stream_input_placeholder_share: "Indsæt nøglen til det stream, du vil dele",
    stream_input_placeholder_join: "Indsæt nøglen til det stream, du vil deltage i",

    stream_button_stream_start: "Start stream",
    stream_button_stream_join: "Deltag i stream",
  },

  available_streams: {
    title: "Aktuelle streams",
    stream_join_message: "Klik på et stream for at deltage",
    no_streams_message: "Ingen streams tilgængelige i øjeblikket",
  },

  statistics: {
    title: "Statistik",
    no_statistics_available: "Ingen statistik tilgængelig i øjeblikket",
    no_sessions: "Ingen sessioner tilgængelige",

    whep_sessions: "WHEP-sessioner",

    video: "Video",
    video_tracks: "Videospor",
    video_track_not_available: "Ingen videospor",
    video_bitrate: "Bitrate",
    video_bitrate_total_out: "Bitrate ud",

    audio: "Lyd",
    audio_tracks: "Lydspor",
    audio_track_not_available: "Ingen lydspor",

    button_watch_stream: "Se stream",

    rid: "RID",
    layer: "Lag",
    packets_received: "Modtagne pakker",
    packets_dropped: "Tabte pakker",
    packets_written: "Sendte pakker",
    last_key_frame: "Sidste nøglebillede",
    timestamp: "Tidsstempel",
    sequence_number: "Sekvensnummer"
  },

  player_header: {
    error: "Fejl",
    success: "Succes",
    warning: "Advarsel",

    mediaAccessError_default: "Kunne ikke få adgang til dine medieenheder",
    mediaAccessError_noMediaDevices: "MediaDevices API blev ikke fundet. Udgivelse i Broadcast Box kræver HTTPS",
    mediaAccessError_notAllowedError: "Du kan ikke streame med dit kamera, adgangen er blevet deaktiveret.",
    mediaAccessError_notFoundError: "Det ser ud til, at du ikke har et kamera, eller at adgangen er blokeret\nTjek kameraindstillinger, browser- og systemtilladelser.",

    connection_established: "Live: Streamer i øjeblikket til",
    connection_disconnected: "WebRTC er afbrudt eller kunne ikke oprette forbindelse",
    connection_failed: "Kunne ikke starte Broadcast Box-session",
    connection_has_packetloss: "WebRTC oplever pakketab",

    publish_screen: "Del skærm/vindue/faneblad",
    publish_webcam: "Del webcam",

    button_end_stream: "Afslut stream"
  },

  player_page: {
    cinema_mode_disable: "Deaktiver biotilstand",
    cinema_mode_enable: "Aktiver biotilstand",

    modal_add_stream_title: "Tilføj stream",
    modal_add_stream_message: "Indsæt stream-nøgle for at tilføje til multi-stream",
    modal_add_stream_placeholder: "Indsæt nøglen til den stream, du vil tilføje",

  },

  player: {
    message_is_not_online: "streamer ikke i øjeblikket",
    message_loading_video: "Indlæser video",
    message_error: "Fejl",

    stream_status_offline: "Offline"
  },

  stream_status: {
    message_current_viewers: "Nuværende seere"
  },

  profile_settings: {
    title: "Profilindstillinger",
    subTitle: "Konfigurer streamingprofil",

    toggle_stream_privacy_label: "Privatliv",
    toggle_stream_privacy_title_left: "Privat",
    toggle_stream_privacy_title_right: "Offentlig",

    input_motd_label: "Dagens besked",
    button_save_label: "Gem"
  },

  admin_login: {
    login_input_dialog_title: "Login",
    login_input_dialog_message: "Indsæt admin-token for at logge ind",
    login_input_dialog_placeholder: "Indsæt admin-token for at logge ind",
    error_message_login_failed: "Login mislykkedes",
    button_login_text: "Login"
  },

  admin_page: {
    title: "Adminportal",
    menu_api: "API",
    menu_logging: "Logning",
    menu_logout: "Log ud",
    menu_profiles: "Profiler",
    menu_settings: "Indstillinger",
    menu_status: "Status"
  },

  admin_page_api: {
    title: "API-indstillinger",
    table_header_setting_name: "Indstilling",
    table_header_value: "Værdi"
  },

  admin_page_logging: {
    title: "Logning",
    table_header_setting_name: "Indstilling",
    table_header_value: "Værdi"
  },

  admin_page_profiles: {
    title: "Profiloversigt",

    add_profile_modal_title: "Tilføj profil",
    add_profile_modal_message: "Indsæt en nøgle for at tilføje en ny stream",
    add_profile_modal_placeholder: "Skriv ny stream-nøgle her",

    remove_profile_modal_title: "Fjern profil",
    remove_profile_modal_message: "Er du sikker på, at du vil fjerne",

    table_header_stream_key: "Stream-nøgle",
    table_header_is_public: "Er offentlig",
    table_header_motd: "Motd",
    table_header_token: "Token",

    button_add_profile: "Tilføj profil",

    yes: "Ja",
    no: "Nej"
  },

  admin_page_status_page: {
    title: "Stream-statusoversigt",

    table_header_stream_key: "Stream-nøgle",
    table_header_is_public: "Er offentlig",
    table_header_video_tracks: "Videospor",
    table_header_audio_tracks: "Lydspor",
    table_header_sessions: "Sessioner",
    table_header_total_packets: "Samlede pakker",

    yes: "ja",
    no: "nej"
  },

  shared_component_card: {
    button_accept: "Acceptér"
  },
  shared_component_text_input_modal: {
    button_accept: "Acceptér"
  },
  shared_component_text_input_dialog: {
    button_accept: "Acceptér"
  },

  chat: {
    title: "Chat",
    placeholder_input: "Skriv en besked",
    button_reaction_title: "Send reaktion",
    button_change_display_name_title: "Skift visningsnavn",
    button_send_title: "Send besked",
    status_connecting: "forbinder",
    status_connected: "forbundet",
    status_error: "fejl",
    status_disconnected: "afbrudt",
    no_messages_yet: "Ingen chatbeskeder endnu.",
    error_failed_to_connect: "Kunne ikke forbinde til chat",
    error_not_connected: "Chat er ikke forbundet",
    error_failed_to_send: "Kunne ikke sende besked",
    modal_display_name_title: "Visningsnavn",
    modal_display_name_message: "Indstil dit visningsnavn til chat",
    modal_display_name_placeholder: "Indtast visningsnavn",
    button_emoji_title: "Emojis og emotes",
    emoji_search_placeholder: "Søg efter emojis",
    emoji_tab_emoji: "Emoji",
    emoji_tab_emotes: "Emotes",
    emoji_no_results: "Intet fundet",
    emotes_not_configured: "Der er ingen 7TV-, BetterTTV- eller FrankerFaceZ-emotes til denne stream.",
    error_rate_limited: "Du sender beskeder for hurtigt",
    emoji_tab_gifs: "GIF'er",
    emote_search_placeholder: "Søg efter emotes på 7TV, BetterTTV og FFZ",
    picker_frequently_used: "Ofte brugt",
    emotes_channel: "Kanalens emotes",
    emotes_matching: "Matchende emotes",
    emotes_search_results: "Fra 7TV, BetterTTV og FrankerFaceZ",
    emotes_searching: "Søger…",
    emotes_search_hint: "Skriv for at søge efter emotes på 7TV, BetterTTV og FrankerFaceZ.",
    gifs_disabled: "GIF'er er ikke slået til på denne server.",
    gif_link_placeholder: "Indsæt et GIF-link",
    gif_add: "Tilføj",
    gif_host_not_allowed: "GIF'er fra denne side er ikke tilladt her.",
    gif_search_placeholder: "Søg efter GIF'er",
    gifs_trending: "Populære",
    gifs_search_results: "Resultater",
    gifs_powered_by: "Leveret af GIPHY",
    gif_press_enter_giphy: "Tryk Enter for også at søge på GIPHY",
    gifs_latest: "Nyeste fra {source}",
    gif_giphy_limited: "GIPHY-søgning er sat på pause et stykke tid, for mange søgninger i denne time.",
    button_reaction_hold_hint: "Klik for at reagere, hold for at vælge en reaktion",
  },
  clips: {
    title: "Klip",
    button_toggle_title: "Vis klip",
    button_create_title: "Lav klip",
    no_clips_yet: "Ingen klip endnu. Brug saksen mens streamen er live for at lave et.",
    loading: "Indlæser klip…",
    error_loading: "Kunne ikke indlæse klip",
    button_download: "Download",
    button_copy_link: "Kopiér link",
    link_copied: "Link kopieret",
    button_delete: "Slet",
    confirm_delete: "Slet dette klip?",
    editor_title: "Lav klip",
    editor_preparing: "Henter de sidste minutter af streamen…",
    editor_selection: "Udvalg",
    editor_start: "Start",
    editor_end: "Slut",
    editor_length: "Længde",
    editor_max_length: "Klip kan højst være {seconds} sekunder lange",
    editor_preview_selection: "Afspil udvalg",
    editor_title_label: "Titel",
    editor_title_placeholder: "Lad feltet være tomt for at bruge dato og tid",
    editor_publish: "Udgiv klip",
    editor_publishing: "Udgiver…",
    editor_cancel: "Annullér",
    editor_close: "Luk",
    editor_published: "Klip udgivet!",
    editor_preview_unsupported: "Din browser kan ikke vise dette klip, men du kan stadig vælge et udsnit og udgive det.",
    error_nothing_recorded: "Der er intet at klippe endnu, streamen skal have været live et par sekunder.",
    error_rate_limited: "Du laver klip for hurtigt, prøv igen om et par sekunder.",
    error_draft_expired: "Dette klipudkast er udløbet, lav venligst et nyt klip.",
    error_generic: "Noget gik galt under oprettelsen af klippet.",
  },

  reactions: {
    button_title: "Reagér",
  },
}
export default locale_da
