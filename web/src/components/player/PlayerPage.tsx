import { useCallback, useContext, useEffect, useState } from "react";
import ReactGridLayout, { useContainerWidth } from "react-grid-layout";
import Player, { type ReactionSender } from "./Player";
import { useNavigate } from "react-router";
import { CinemaModeContext } from "../../providers/CinemaModeProvider";
import ModalTextInput from "../shared/ModalTextInput";
import Button from "../shared/Button";
import AvailableStreams from "../selection/AvailableStreams";
import { LocaleContext } from "../../providers/LocaleProvider";
import ChatPanel from "./components/ChatPanel";
import { ChatAdapter } from "../../hooks/useChatSession";
import { StreamMOTD } from "./components/StreamMOTD";
import { StreamStatus } from "../../providers/StatusProvider";
import ClipsPanel from "./components/ClipsPanel";
import ClipEditor from "./components/ClipEditor";
import { getClipsConfig } from "./functions/clipsApi";

import "react-grid-layout/css/styles.css";
import "react-resizable/css/styles.css";

const PlayerPage = () => {
  const navigate = useNavigate();
  const { width: playerGridWidth, containerRef: playerGridRef } = useContainerWidth();
  const { locale } = useContext(LocaleContext);
  const { cinemaMode, toggleCinemaMode } = useContext(CinemaModeContext);
  const [streamKeys, setStreamKeys] = useState<string[]>([window.location.pathname.substring(1)]);
  const [isModalOpen, setIsModelOpen] = useState<boolean>(false);
  const [isChatOpen, setIsChatOpen] = useState<boolean>(() => localStorage.getItem("chat-open") !== "false");
  const [isClipsOpen, setIsClipsOpen] = useState<boolean>(() => localStorage.getItem("clips-open") === "true");
  const [clipsEnabled, setClipsEnabled] = useState<boolean>(false);
  const [clipEditorStreamKey, setClipEditorStreamKey] = useState<string>();
  const [chatAdapters, setChatAdapters] = useState<Record<string, ChatAdapter | undefined>>({});
  const [reactionSenders, setReactionSenders] = useState<Record<string, ReactionSender | undefined>>({});
  const [streamStatuses, setStreamStatuses] = useState<Record<string, StreamStatus | undefined>>({});
  const [isDisplayNameModalOpen, setIsDisplayNameModalOpen] = useState<boolean>(false);
  const [chatDisplayName, setChatDisplayName] = useState<string>(() => localStorage.getItem("chatDisplayName") ?? "");

  useEffect(() => {
    localStorage.setItem("chat-open", String(isChatOpen));
  }, [isChatOpen]);

  useEffect(() => {
    localStorage.setItem("clips-open", String(isClipsOpen));
  }, [isClipsOpen]);

  useEffect(() => {
    getClipsConfig().then((config) => setClipsEnabled(config.enabled));
  }, []);

  const addStream = (streamKey: string) => {
    if (streamKeys.some((key: string) => key.toLowerCase() === streamKey.toLowerCase())) {
      return;
    }
    setStreamKeys((prev) => [...prev, streamKey]);
    setIsModelOpen((prev) => !prev);
  };

  const setStreamChatAdapter = useCallback((streamKey: string, adapter: ChatAdapter | undefined) => {
    setChatAdapters((current) => {
      if (current[streamKey] === adapter) {
        return current;
      }

      return {
        ...current,
        [streamKey]: adapter,
      };
    });
  }, []);

  const setStreamReactionSender = useCallback((streamKey: string, sender: ReactionSender | undefined) => {
    setReactionSenders((current) => {
      if (current[streamKey] === sender) {
        return current;
      }

      return {
        ...current,
        [streamKey]: sender,
      };
    });
  }, []);

  const setStreamStatus = useCallback((streamKey: string, status: StreamStatus | undefined) => {
    setStreamStatuses((current) => {
      if (current[streamKey] === status) {
        return current;
      }

      return {
        ...current,
        [streamKey]: status,
      };
    });
  }, []);

  const removeStream = (streamKey: string) => {
    setStreamKeys((prev) => prev.filter((key) => key !== streamKey));
    setChatAdapters((current) => {
      const next = { ...current };
      delete next[streamKey];
      return next;
    });
    setReactionSenders((current) => {
      const next = { ...current };
      delete next[streamKey];
      return next;
    });
    setStreamStatuses((current) => {
      const next = { ...current };
      delete next[streamKey];
      return next;
    });
  };

  const saveDisplayName = useCallback((value: string) => {
    const trimmedValue = value.trim();
    if (!trimmedValue) {
      return;
    }
    setChatDisplayName(trimmedValue);
    localStorage.setItem("chatDisplayName", trimmedValue);
    setIsDisplayNameModalOpen(false);
  }, []);

  const isSingleStream = streamKeys.length === 1;
  const playerGridColumns = isSingleStream ? 1 : 2;
  const playerGridGap = 8;
  const playerGridRowHeight = 16;
  const playerGridItemPixelWidth = (playerGridWidth - playerGridGap * (playerGridColumns - 1)) / playerGridColumns;
  const isMobilePlayer = window.innerWidth < 768;
  const playerGridItemWidth = 12 / playerGridColumns;
  const isClipsPanelOpen = clipsEnabled && isClipsOpen;
  const isSidePanelOpen = isChatOpen || isClipsPanelOpen;
  const isSingleStreamChatSidebar = isSingleStream && isSidePanelOpen && playerGridItemPixelWidth >= 1024;
  const chatSidebarWidth = cinemaMode ? 320 : 336;
  const clipsBelowHeight = 264;
  const chatBelowHeight = isSingleStreamChatSidebar ? 0 :
    (isChatOpen ? (isSingleStream ? 336 : 388) : 0) + (isClipsPanelOpen ? clipsBelowHeight : 0);
  const playerWidth = Math.max(0, playerGridItemPixelWidth - (isSingleStreamChatSidebar ? chatSidebarWidth : 0));
  const playerGridCardHeight = Math.ceil(playerWidth * 9 / 16 + 24 + chatBelowHeight);
  const playerGridCardRows = Math.max(1, Math.ceil((playerGridCardHeight + playerGridGap) / (playerGridRowHeight + playerGridGap)));
  const chatPanelVariant = isSingleStreamChatSidebar ? "fill" : (isSingleStream ? "compact-below" : "below");

  return (
    <div>
      {clipEditorStreamKey && (
        <ClipEditor
          streamKey={clipEditorStreamKey}
          onClose={() => setClipEditorStreamKey(undefined)}
        />
      )}

      {isModalOpen && (
        <ModalTextInput<string>
          title={locale.player_page.modal_add_stream_title}
          message={locale.player_page.modal_add_stream_message}
          placeholder={locale.player_page.modal_add_stream_placeholder}
          isOpen={isModalOpen}
          canCloseOnBackgroundClick={true}
          onClose={() => setIsModelOpen(false)}
          onAccept={(result: string) => addStream(result)}
        >
          <AvailableStreams
            showHeader={false}
            onClickOverride={(streamKey) => addStream(streamKey)}
          />
        </ModalTextInput>
      )}

      {isDisplayNameModalOpen && (
        <ModalTextInput<string>
          title={locale.chat.modal_display_name_title}
          message={locale.chat.modal_display_name_message}
          placeholder={locale.chat.modal_display_name_placeholder}
          isOpen={isDisplayNameModalOpen}
          canCloseOnBackgroundClick
          onClose={() => setIsDisplayNameModalOpen(false)}
          onAccept={saveDisplayName}
        />
      )}

      <div className={`flex flex-col w-full items-center ${!cinemaMode && "mx-auto px-2 py-2 container"}`} >
        <div ref={playerGridRef} className="w-full">
          <ReactGridLayout
            dragConfig={{
              enabled: !isMobilePlayer,
              handle: ".player-drag-handle",
              cancel: ".player-drag-cancel,button,input,select,textarea,a,[role='button']",
            }}
            resizeConfig={{ enabled: !isMobilePlayer }}
            layout={streamKeys.map((streamKey, index) => ({
              i: `${streamKey}_player_card`,
              x: (index % playerGridColumns) * playerGridItemWidth,
              y: Math.floor(index / playerGridColumns) * playerGridCardRows,
              w: playerGridItemWidth,
              h: playerGridCardRows,
            }))}
            width={playerGridWidth}
            gridConfig={{ cols: 12, rowHeight: playerGridRowHeight, margin: [playerGridGap, playerGridGap], containerPadding: [0, 0] }}
          >
            {streamKeys.map((streamKey) => {
              return (
                <div key={`${streamKey}_player_card`} className="min-w-0 flex h-full flex-col gap-1 overflow-hidden">
                  <div className={isSingleStream ? "relative flex min-h-0 flex-1 flex-col gap-4 w-full" : "flex min-h-0 flex-1 flex-col gap-1"}>
                    <div className={isSingleStream ? `min-w-0 min-h-0 flex-1 transition-[margin] duration-200 ${isSingleStreamChatSidebar ? (cinemaMode ? "mr-80" : "mr-[21rem]") : ""}` : "min-w-0 min-h-0 flex-1"}>
                      <Player
                        key={`${streamKey}_player`}
                        streamKey={streamKey}
                        cinemaMode={cinemaMode}
                        fillContainer
                        isChatOpen={isChatOpen}
                        onToggleChat={() => setIsChatOpen((prev) => !prev)}
                        onChatAdapterChange={setStreamChatAdapter}
                        onReactionSenderChange={setStreamReactionSender}
                        onStreamStatusChange={setStreamStatus}
                        onCloseStream={isSingleStream ? () => navigate("/") : () => removeStream(streamKey)}
                        isClipsOpen={isClipsPanelOpen}
                        onToggleClips={clipsEnabled ? () => setIsClipsOpen((prev) => !prev) : undefined}
                        onCreateClip={clipsEnabled ? () => setClipEditorStreamKey(streamKey) : undefined}
                      />
                    </div>

                    {/* Clips above chat: one column beside the player, or stacked below it */}
                    <div className={isSingleStreamChatSidebar
                      ? `absolute top-0 right-0 flex h-full flex-col gap-2 ${cinemaMode ? "w-80" : "w-[21rem]"}`
                      : "flex flex-col gap-1"}>
                      {isClipsPanelOpen && (
                        <ClipsPanel
                          streamKey={streamKey}
                          className={isSingleStreamChatSidebar ? "min-h-0 flex-1" : "h-64 shrink-0"}
                        />
                      )}

                      <ChatPanel
                        streamKey={streamKey}
                        variant={chatPanelVariant}
                        isOpen={isChatOpen}
                        adapter={chatAdapters[streamKey]}
                        displayName={chatDisplayName}
                        onReaction={reactionSenders[streamKey]}
                        onChangeDisplayNameRequested={() => setIsDisplayNameModalOpen(true)}
                      />
                    </div>
                  </div>

                  <StreamMOTD
                    isOnline={streamStatuses[streamKey]?.isOnline ?? false}
                    motd={streamStatuses[streamKey]?.motd ?? ""}
                    className={isSingleStream ? "px-1" : "px-4"}
                  />
                </div>
              );
            })}
          </ReactGridLayout>
        </div>

        {/*Footer menu*/}
        <div className="mt-4 flex flex-row gap-2">
          <Button
            title={cinemaMode ? locale.player_page.cinema_mode_disable : locale.player_page.cinema_mode_enable}
            onClick={toggleCinemaMode}
            iconRight="CodeBracketSquare"
          />

          {/*Show modal to add stream keys with*/}
          <Button
            title={locale.player_page.modal_add_stream_title}
            color="Accept"
            onClick={() => setIsModelOpen((prev) => !prev)}
            iconRight="SquaresPlus"
          />
        </div>
      </div>
    </div>
  );
};

export default PlayerPage;
