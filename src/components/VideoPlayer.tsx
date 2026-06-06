import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type MutableRefObject,
} from "react";
import Artplayer, { type Option } from "artplayer";
import type Hls from "hls.js";

type Props = {
  id?: string;
  src: string;
  poster: string;
  previewSrc?: string;
  title: string;
  /**
   * 用户首次按下播放时触发。同一个 VideoPlayer 实例只会触发一次；
   * 后续暂停-继续不会重复触发。换 src 时会重置（详情页切换视频用）。
   */
  onFirstPlay?: () => void;
};

type ResumePrompt = {
  time: number;
};

type PlayerError = {
  title: string;
  message: string;
};

type GestureHud = {
  key: number;
  label: string;
};

type PreviewHover = {
  x: number;
  ratio: number;
  time: number;
};

type PlayerSettings = {
  volume: number;
  muted: boolean;
  playbackRate: number;
};

type PlaybackRecord = {
  time: number;
  duration: number;
  updatedAt: number;
};

type VideoElementWithHls = HTMLVideoElement & {
  __hls?: Hls | null;
};

type OrientationMode = "landscape" | "portrait";
type OrientationKind = "native" | "web";
type FullscreenElement = HTMLElement & {
  webkitRequestFullscreen?: () => Promise<void> | void;
  mozRequestFullScreen?: () => Promise<void> | void;
  msRequestFullscreen?: () => Promise<void> | void;
};
type FullscreenDocument = Document & {
  webkitFullscreenElement?: Element | null;
  mozFullScreenElement?: Element | null;
  msFullscreenElement?: Element | null;
  webkitExitFullscreen?: () => Promise<void> | void;
  mozCancelFullScreen?: () => Promise<void> | void;
  msExitFullscreen?: () => Promise<void> | void;
};
type LockableScreenOrientation = ScreenOrientation & {
  lock?: (orientation: OrientationMode) => Promise<void>;
  unlock?: () => void;
};

/** 长按多少毫秒后进入 2 倍速。短按属于普通点击，交给 ArtPlayer 处理。 */
const LONG_PRESS_MS = 400;
/** 长按时使用的播放倍速。 */
const FAST_RATE = 2;
/** 默认倍速。 */
const NORMAL_RATE = 1;

Artplayer.FAST_FORWARD_VALUE = FAST_RATE;

const SETTINGS_KEY = "video-site:player-settings";
const PLAYBACK_KEY_PREFIX = "video-site:playback:";
const DEFAULT_SETTINGS: PlayerSettings = {
  volume: 0.7,
  muted: false,
  playbackRate: 1,
};
const ORIENTATION_CONTROL_NAME = "orientationToggle";
const MANUAL_ORIENTATION_CLASS = "art-manual-orientation";
const RESUME_MIN_SECONDS = 10;
const RESUME_END_GAP_SECONDS = 12;
const PREVIEW_WIDTH = 168;

export function VideoPlayer({
  id,
  src,
  poster,
  previewSrc,
  title,
  onFirstPlay,
}: Props) {
  const mountRef = useRef<HTMLDivElement | null>(null);
  const artRef = useRef<Artplayer | null>(null);
  const previewVideoRef = useRef<HTMLVideoElement | null>(null);
  const onFirstPlayRef = useRef<Props["onFirstPlay"]>(onFirstPlay);
  const playedRef = useRef(false);
  const videoKey = id || src;
  const [fastActive, setFastActive] = useState(false);
  const [retryNonce, setRetryNonce] = useState(0);
  const [resumePrompt, setResumePrompt] = useState<ResumePrompt | null>(null);
  const [playerError, setPlayerError] = useState<PlayerError | null>(null);
  const [gestureHud, setGestureHud] = useState<GestureHud | null>(null);
  const [previewHover, setPreviewHover] = useState<PreviewHover | null>(null);

  useEffect(() => {
    onFirstPlayRef.current = onFirstPlay;
  }, [onFirstPlay]);

  useEffect(() => {
    const mount = mountRef.current;
    if (!mount) return;

    playedRef.current = false;
    setFastActive(false);
    setResumePrompt(null);
    setPlayerError(null);
    setPreviewHover(null);

    const cleanupPlayer = mountArtPlayer({
      mount,
      videoKey,
      src,
      poster,
      title,
      artRef,
      playedRef,
      onFirstPlayRef,
      onFastChange: setFastActive,
      onResumeAvailable: setResumePrompt,
      onError: setPlayerError,
      onPreviewHover: setPreviewHover,
    });

    return cleanupPlayer;
  }, [poster, retryNonce, src, title, videoKey]);

  useEffect(() => {
    if (!previewSrc || !previewHover) return;
    syncPreviewVideo(previewVideoRef.current, previewHover.ratio);
  }, [previewHover, previewSrc]);

  function continuePlayback() {
    const video = artRef.current?.video;
    if (!video || !resumePrompt) return;
    try {
      video.currentTime = resumePrompt.time;
    } catch {
      // ignore
    }
    setResumePrompt(null);
  }

  function restartPlayback() {
    const video = artRef.current?.video;
    if (video) {
      try {
        video.currentTime = 0;
      } catch {
        // ignore
      }
    }
    clearPlaybackRecord(videoKey);
    setResumePrompt(null);
  }

  function retryPlayback() {
    setPlayerError(null);
    setRetryNonce((n) => n + 1);
  }

  async function copySource() {
    const absolute = new URL(src, window.location.href).href;
    try {
      await navigator.clipboard.writeText(absolute);
      showTransientHud(setGestureHud, "播放地址已复制");
    } catch {
      fallbackCopyText(absolute);
      showTransientHud(setGestureHud, "播放地址已复制");
    }
  }

  const previewStyle = previewHover
    ? ({ left: `${previewHover.x}px` } as CSSProperties)
    : undefined;

  return (
    <div className="video-player">
      <div
        className="video-player__poster-bg"
        style={{ backgroundImage: poster ? `url(${poster})` : undefined }}
        aria-hidden="true"
      />
      <div ref={mountRef} className="video-player__mount" />

      {resumePrompt && !playerError && (
        <div className="video-player__resume" role="status">
          <span>上次播放到 {formatClock(resumePrompt.time)}</span>
          <button type="button" onClick={continuePlayback}>
            继续播放
          </button>
          <button type="button" onClick={restartPlayback}>
            从头播放
          </button>
        </div>
      )}

      {playerError && (
        <div className="video-player__error" role="alert">
          <div className="video-player__error-title">{playerError.title}</div>
          <div className="video-player__error-message">{playerError.message}</div>
          <div className="video-player__error-actions">
            <button type="button" onClick={retryPlayback}>
              重试
            </button>
            <button type="button" onClick={copySource}>
              复制地址
            </button>
          </div>
        </div>
      )}

      {previewSrc && previewHover && (
        <div
          className="video-player__seek-preview"
          style={previewStyle}
          aria-hidden="true"
        >
          <video
            ref={previewVideoRef}
            src={previewSrc}
            poster={poster}
            muted
            playsInline
            preload="metadata"
            onLoadedMetadata={() =>
              syncPreviewVideo(previewVideoRef.current, previewHover.ratio)
            }
          />
          <span>{formatClock(previewHover.time)}</span>
        </div>
      )}

      {gestureHud && (
        <div
          key={gestureHud.key}
          className="video-player__gesture-hud"
          aria-hidden="true"
        >
          {gestureHud.label}
        </div>
      )}

      {fastActive && (
        <div className="video-player__rate-hint" aria-hidden="true">
          2x
        </div>
      )}
    </div>
  );
}

function inferSourceType(src: string) {
  const lower = src.toLowerCase();
  const cleanPath = lower.split("#")[0].split("?")[0];
  if (cleanPath.endsWith(".m3u8") || lower.includes(".m3u8")) return "m3u8";
  return undefined;
}

function mountArtPlayer({
  mount,
  videoKey,
  src,
  poster,
  title,
  artRef,
  playedRef,
  onFirstPlayRef,
  onFastChange,
  onResumeAvailable,
  onError,
  onPreviewHover,
}: {
  mount: HTMLDivElement;
  videoKey: string;
  src: string;
  poster: string;
  title: string;
  artRef: MutableRefObject<Artplayer | null>;
  playedRef: MutableRefObject<boolean>;
  onFirstPlayRef: MutableRefObject<Props["onFirstPlay"]>;
  onFastChange: (active: boolean) => void;
  onResumeAvailable: (prompt: ResumePrompt | null) => void;
  onError: (error: PlayerError | null) => void;
  onPreviewHover: (hover: PreviewHover | null) => void;
}) {
  const sourceType = inferSourceType(src);
  const settings = readPlayerSettings();
  const fastActiveRef = { current: false };
  const loadHlsSource = createHlsSourceLoader(onError);
  const option: Option = {
    id: "91-detail-player",
    container: mount,
    url: src,
    poster,
    theme: "var(--video-player-progress)",
    lang: "zh-cn",
    volume: settings.volume,
    muted: settings.muted,
    autoplay: false,
    autoSize: false,
    playbackRate: true,
    aspectRatio: true,
    setting: true,
    hotkey: true,
    pip: true,
    mutex: true,
    fullscreen: true,
    fullscreenWeb: true,
    miniProgressBar: true,
    backdrop: true,
    playsInline: true,
    lock: true,
    gesture: true,
    fastForward: true,
    airplay: true,
    customType: {
      hls: loadHlsSource,
      m3u8: loadHlsSource,
    },
    moreVideoAttr: {
      preload: "metadata",
    },
    controls: [createOrientationControl()],
    contextmenu: [],
    cssVar: {
      "--art-theme": "var(--video-player-progress)",
    },
  };
  if (sourceType) {
    option.type = sourceType;
  }

  const art = new Artplayer(option);
  artRef.current = art;

  const video = art.video as VideoElementWithHls;
  video.setAttribute("aria-label", title);
  video.setAttribute("controlsList", "nodownload");
  video.disablePictureInPicture = false;
  video.playbackRate = settings.playbackRate;

  function preventContextMenu(event: Event) {
    event.preventDefault();
  }

  function handlePlay() {
    if (!playedRef.current) {
      playedRef.current = true;
      onFirstPlayRef.current?.();
    }
    onError(null);
  }

  function handleLoadStart() {
    onError(null);
  }

  function handleReady() {
    onError(null);
  }

  function handleVideoError() {
    onError({
      title: "视频源加载失败",
      message: mediaErrorMessage(video.error),
    });
  }

  function resetFastRate() {
    fastActiveRef.current = false;
    onFastChange(false);
  }

  function handleEnded() {
    resetFastRate();
    clearPlaybackRecord(videoKey);
  }

  function handleLoadedMetadata() {
    maybeOfferResume(videoKey, video, onResumeAvailable);
  }

  function handleTimeUpdate() {
    savePlaybackRecord(videoKey, video);
  }

  function handleVolumeChange() {
    writePlayerSettings({
      volume: clamp(video.volume, 0, 1),
      muted: video.muted,
    });
  }

  function handleRateChange() {
    if (fastActiveRef.current) return;
    if (!Number.isFinite(video.playbackRate)) return;
    writePlayerSettings({
      playbackRate: clamp(video.playbackRate, 0.5, 3),
    });
  }

  const unbindFastRate = bindLongPressFast(video, (active) => {
    fastActiveRef.current = active;
    onFastChange(active);
  });
  const unbindProgressPreview = bindProgressPreview(
    art,
    video,
    mount,
    onPreviewHover
  );
  const unbindOrientationToggle = bindOrientationToggle(art);

  mount.addEventListener("contextmenu", preventContextMenu);
  video.addEventListener("loadedmetadata", handleLoadedMetadata);
  video.addEventListener("timeupdate", handleTimeUpdate);
  video.addEventListener("volumechange", handleVolumeChange);
  video.addEventListener("ratechange", handleRateChange);

  art.on("video:loadstart", handleLoadStart);
  art.on("video:loadeddata", handleReady);
  art.on("video:canplay", handleReady);
  art.on("video:playing", handleReady);
  art.on("video:error", handleVideoError);
  art.on("error", handleVideoError);
  art.on("video:play", handlePlay);
  art.on("video:pause", resetFastRate);
  art.on("video:ended", handleEnded);

  return () => {
    unbindFastRate();
    unbindProgressPreview();
    unbindOrientationToggle();
    mount.removeEventListener("contextmenu", preventContextMenu);
    video.removeEventListener("loadedmetadata", handleLoadedMetadata);
    video.removeEventListener("timeupdate", handleTimeUpdate);
    video.removeEventListener("volumechange", handleVolumeChange);
    video.removeEventListener("ratechange", handleRateChange);
    destroyHls(video);
    art.off("video:loadstart", handleLoadStart);
    art.off("video:loadeddata", handleReady);
    art.off("video:canplay", handleReady);
    art.off("video:playing", handleReady);
    art.off("video:error", handleVideoError);
    art.off("error", handleVideoError);
    art.off("video:play", handlePlay);
    art.off("video:pause", resetFastRate);
    art.off("video:ended", handleEnded);
    art.destroy(true);
    if (artRef.current === art) {
      artRef.current = null;
    }
    onPreviewHover(null);
  };
}

function createOrientationControl(): NonNullable<Option["controls"]>[number] {
  return {
    name: ORIENTATION_CONTROL_NAME,
    position: "right",
    index: 55,
    tooltip: "横竖屏切换",
    html: `
      <span class="video-player__orientation-control-icon video-player__orientation-control-icon--to-landscape" aria-hidden="true">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
          <path d="M14.4 11.2h2.7c1.7 0 3 1.3 3 3v4.1c0 1.7-1.3 3-3 3h-3.8" fill="none" stroke="currentColor" stroke-opacity=".42" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round"/>
          <rect x="3.1" y="6.7" width="9.7" height="14.1" rx="2.4" fill="none" stroke="currentColor" stroke-width="2.3"/>
          <path d="M11.8 2.8h2.9c2.6 0 4.7 1.8 5 4.2" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round"/>
          <path d="M17.4 4.6 19.8 7 22 4.5" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
      </span>
      <span class="video-player__orientation-control-icon video-player__orientation-control-icon--to-portrait" aria-hidden="true">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
          <g transform="rotate(180 12 12)">
            <path d="M12.8 14.4v2.7c0 1.7-1.3 3-3 3H5.7c-1.7 0-3-1.3-3-3v-3.8" fill="none" stroke="currentColor" stroke-opacity=".42" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round"/>
            <rect x="3.2" y="3.1" width="14.1" height="9.7" rx="2.4" fill="none" stroke="currentColor" stroke-width="2.3"/>
            <path d="M21.2 11.8v2.9c0 2.6-1.8 4.7-4.2 5" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round"/>
            <path d="M19.4 17.4 17 19.8 19.5 22" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round"/>
          </g>
        </svg>
      </span>
    `,
    mounted(element) {
      element.setAttribute("role", "button");
      element.setAttribute("tabindex", "0");
      updateOrientationControl(this, element);
      this.events.proxy(element, "keydown", (event) => {
        const keyEvent = event as KeyboardEvent;
        if (keyEvent.key !== "Enter" && keyEvent.key !== " ") return;
        keyEvent.preventDefault();
        void togglePlayerOrientation(this);
      });
    },
    click() {
      void togglePlayerOrientation(this);
    },
  };
}

function bindOrientationToggle(art: Artplayer) {
  function handleResize() {
    updateManualWebOrientation(art);
    updateOrientationControl(art);
  }

  function handleFullscreenWeb(state: boolean) {
    if (!state && getManualOrientationKind(art) === "web") {
      clearManualOrientation(art);
      return;
    }
    handleResize();
  }

  function handleFullscreen(state: boolean) {
    if (!state && getManualOrientationKind(art) === "native") {
      clearManualOrientation(art);
      return;
    }
    updateOrientationControl(art);
  }

  window.addEventListener("resize", handleResize);
  window.addEventListener("orientationchange", handleResize);
  getScreenOrientation()?.addEventListener?.("change", handleResize);
  art.on("fullscreenWeb", handleFullscreenWeb);
  art.on("fullscreen", handleFullscreen);
  updateOrientationControl(art);

  return () => {
    clearManualOrientation(art);
    window.removeEventListener("resize", handleResize);
    window.removeEventListener("orientationchange", handleResize);
    getScreenOrientation()?.removeEventListener?.("change", handleResize);
    art.off("fullscreenWeb", handleFullscreenWeb);
    art.off("fullscreen", handleFullscreen);
  };
}

async function togglePlayerOrientation(art: Artplayer) {
  const target = nextOrientationTarget(art);
  const locked = await lockNativeOrientation(art, target);
  if (locked) {
    clearManualWebRotation(art);
    setManualOrientation(art, target, "native");
    art.notice.show = `已切换${orientationLabel(target)}`;
    updateOrientationControl(art);
    return;
  }

  await exitNativeFullscreen();
  if (!art.fullscreenWeb) {
    art.fullscreenWeb = true;
  }
  setManualOrientation(art, target, "web");
  updateManualWebOrientation(art);
  art.notice.show = `已切换${orientationLabel(target)}`;
  updateOrientationControl(art);
}

async function lockNativeOrientation(
  art: Artplayer,
  target: OrientationMode
) {
  const orientation = getScreenOrientation();
  if (!orientation?.lock) return false;

  try {
    const fullscreen = await requestNativeFullscreen(art.template.$player);
    if (!fullscreen) return false;
    await orientation.lock(target);
    return true;
  } catch {
    return false;
  }
}

async function requestNativeFullscreen(element: HTMLElement) {
  if (getNativeFullscreenElement()) return true;
  const target = element as FullscreenElement;
  try {
    if (target.requestFullscreen) {
      await target.requestFullscreen({ navigationUI: "hide" });
      return true;
    }
    const request =
      target.webkitRequestFullscreen ||
      target.mozRequestFullScreen ||
      target.msRequestFullscreen;
    if (!request) return false;
    await maybePromise(request.call(target));
    return true;
  } catch {
    return false;
  }
}

async function exitNativeFullscreen() {
  if (!getNativeFullscreenElement()) return;
  const doc = document as FullscreenDocument;
  const exit =
    doc.exitFullscreen ||
    doc.webkitExitFullscreen ||
    doc.mozCancelFullScreen ||
    doc.msExitFullscreen;
  if (!exit) return;
  try {
    await maybePromise(exit.call(document));
  } catch {
    // ignore
  }
}

function getNativeFullscreenElement() {
  const doc = document as FullscreenDocument;
  return (
    document.fullscreenElement ||
    doc.webkitFullscreenElement ||
    doc.mozFullScreenElement ||
    doc.msFullscreenElement ||
    null
  );
}

function getScreenOrientation() {
  return window.screen?.orientation as LockableScreenOrientation | undefined;
}

async function maybePromise(value: Promise<void> | void) {
  if (value && typeof value.then === "function") {
    await value;
  }
}

function nextOrientationTarget(art: Artplayer): OrientationMode {
  const active = getManualOrientationTarget(art) ?? getViewportOrientation();
  return active === "landscape" ? "portrait" : "landscape";
}

function getViewportOrientation(): OrientationMode {
  const type = getScreenOrientation()?.type;
  if (type?.startsWith("landscape")) return "landscape";
  if (type?.startsWith("portrait")) return "portrait";
  return window.innerWidth > window.innerHeight ? "landscape" : "portrait";
}

function setManualOrientation(
  art: Artplayer,
  target: OrientationMode,
  kind: OrientationKind
) {
  const { dataset } = art.template.$player;
  dataset.videoPlayerOrientationTarget = target;
  dataset.videoPlayerOrientationKind = kind;
}

function getManualOrientationTarget(art: Artplayer) {
  const value = art.template.$player.dataset.videoPlayerOrientationTarget;
  return value === "landscape" || value === "portrait" ? value : null;
}

function getManualOrientationKind(art: Artplayer) {
  const value = art.template.$player.dataset.videoPlayerOrientationKind;
  return value === "native" || value === "web" ? value : null;
}

function clearManualOrientation(art: Artplayer) {
  const kind = getManualOrientationKind(art);
  delete art.template.$player.dataset.videoPlayerOrientationTarget;
  delete art.template.$player.dataset.videoPlayerOrientationKind;
  clearManualWebRotation(art);
  if (kind === "native") {
    try {
      getScreenOrientation()?.unlock?.();
    } catch {
      // ignore
    }
  }
  updateOrientationControl(art);
}

function updateManualWebOrientation(art: Artplayer) {
  if (getManualOrientationKind(art) !== "web") return;
  const target = getManualOrientationTarget(art);
  if (!target) return;
  if (!art.fullscreenWeb) {
    clearManualOrientation(art);
    return;
  }
  if (target !== getViewportOrientation()) {
    applyManualWebRotation(art);
  } else {
    clearManualWebRotation(art);
  }
}

function applyManualWebRotation(art: Artplayer) {
  const player = art.template.$player;
  const viewWidth = document.documentElement.clientWidth;
  const viewHeight = document.documentElement.clientHeight;
  player.style.width = `${viewHeight}px`;
  player.style.height = `${viewWidth}px`;
  player.style.transformOrigin = "0 0";
  player.style.transform = `rotate(90deg) translate(0, -${viewWidth}px)`;
  player.classList.add(MANUAL_ORIENTATION_CLASS);
  art.emit("resize");
}

function clearManualWebRotation(art: Artplayer) {
  const player = art.template.$player;
  player.classList.remove(MANUAL_ORIENTATION_CLASS);
  player.style.transform = "";
  player.style.transformOrigin = "";
  if (art.fullscreenWeb) {
    player.style.width = "100%";
    player.style.height = "100%";
  } else {
    player.style.width = "";
    player.style.height = "";
  }
  art.emit("resize");
}

function updateOrientationControl(art: Artplayer, mountedElement?: HTMLElement) {
  const controls = (art as Artplayer & {
    controls?: Record<string, HTMLElement | undefined>;
  }).controls;
  const element = mountedElement ?? controls?.[ORIENTATION_CONTROL_NAME];
  if (!element) return;
  const next = nextOrientationTarget(art);
  const label = `切换${orientationLabel(next)}`;
  element.dataset.nextOrientation = next;
  element.setAttribute("aria-label", label);
  element.setAttribute("title", label);
}

function orientationLabel(mode: OrientationMode) {
  return mode === "landscape" ? "横屏" : "竖屏";
}

function createHlsSourceLoader(
  onError: (error: PlayerError | null) => void
) {
  return function loadHlsSource(
    video: HTMLVideoElement,
    url: string,
    art: Artplayer
  ) {
    const target = video as VideoElementWithHls;
    destroyHls(target);
    onError(null);

    void import("hls.js")
      .then((hlsModule) => {
        if (art.isDestroy || !video.isConnected) return;
        loadHlsSourceWith(video, url, art, hlsModule.default, onError);
      })
      .catch(() => {
        if (art.isDestroy) return;
        onError({
          title: "HLS 内核加载失败",
          message: "播放器组件加载失败，请刷新页面后重试。",
        });
      });
  };
}

function loadHlsSourceWith(
  video: HTMLVideoElement,
  url: string,
  art: Artplayer,
  HlsCtor: typeof Hls,
  onError: (error: PlayerError | null) => void
) {
  const target = video as VideoElementWithHls;
  destroyHls(target);

  if (HlsCtor.isSupported()) {
    const hls = new HlsCtor({
      enableWorker: true,
      lowLatencyMode: true,
      backBufferLength: 90,
    });

    target.__hls = hls;
    art.hls = hls;
    hls.loadSource(url);
    hls.attachMedia(video);
    hls.on(HlsCtor.Events.ERROR, (_event, data) => {
      if (!data.fatal) return;

      if (data.type === HlsCtor.ErrorTypes.NETWORK_ERROR) {
        art.notice.show = "网络错误，正在重试";
        hls.startLoad();
        return;
      }

      if (data.type === HlsCtor.ErrorTypes.MEDIA_ERROR) {
        art.notice.show = "媒体错误，正在恢复";
        hls.recoverMediaError();
        return;
      }

      destroyHls(target);
      onError({
        title: "HLS 播放失败",
        message: "当前视频流无法解析，请稍后重试或复制播放地址排查。",
      });
    });
    return;
  }

  if (
    video.canPlayType("application/vnd.apple.mpegurl") ||
    video.canPlayType("application/x-mpegURL")
  ) {
    video.src = url;
    return;
  }

  onError({
    title: "当前浏览器不支持 HLS",
    message: "请换用新版 Chrome、Edge 或 Safari 后再试。",
  });
}

function destroyHls(video: VideoElementWithHls) {
  if (!video.__hls) return;
  video.__hls.destroy();
  video.__hls = null;
}

function bindLongPressFast(
  video: HTMLVideoElement,
  onFastChange: (active: boolean) => void
) {
  let pressTimer: number | null = null;
  let fastActive = false;
  let previousRate = NORMAL_RATE;

  function clearPressTimer() {
    if (pressTimer !== null) {
      window.clearTimeout(pressTimer);
      pressTimer = null;
    }
  }

  function setFast(next: boolean) {
    if (fastActive === next) return;
    if (next) {
      previousRate =
        Number.isFinite(video.playbackRate) && video.playbackRate > 0
          ? video.playbackRate
          : NORMAL_RATE;
    }
    fastActive = next;
    video.playbackRate = next ? FAST_RATE : previousRate;
    onFastChange(next);
  }

  function activateFast() {
    if (video.paused || video.ended) return;
    setFast(true);
  }

  function startPress() {
    if (video.paused || video.ended) return;
    clearPressTimer();
    pressTimer = window.setTimeout(() => {
      pressTimer = null;
      activateFast();
    }, LONG_PRESS_MS);
  }

  function endPress() {
    clearPressTimer();
    setFast(false);
  }

  function handleMouseDown(event: MouseEvent) {
    if (event.button !== 0) return;
    startPress();
  }

  video.addEventListener("mousedown", handleMouseDown);
  video.addEventListener("mouseup", endPress);
  video.addEventListener("mouseleave", endPress);
  video.addEventListener("pause", endPress);
  video.addEventListener("ended", endPress);

  return () => {
    clearPressTimer();
    setFast(false);
    video.removeEventListener("mousedown", handleMouseDown);
    video.removeEventListener("mouseup", endPress);
    video.removeEventListener("mouseleave", endPress);
    video.removeEventListener("pause", endPress);
    video.removeEventListener("ended", endPress);
  };
}

function bindProgressPreview(
  art: Artplayer,
  video: HTMLVideoElement,
  mount: HTMLDivElement,
  onPreviewHover: (hover: PreviewHover | null) => void
) {
  const progress = art.query<HTMLElement>(".art-progress");
  if (!progress) return () => undefined;
  const progressEl = progress;

  function update(event: PointerEvent | MouseEvent) {
    if ("pointerType" in event && event.pointerType === "touch") return;
    const duration = video.duration;
    if (!Number.isFinite(duration) || duration <= 0) return;
    const rect = progressEl.getBoundingClientRect();
    const hostRect = mount.getBoundingClientRect();
    const ratio = clamp((event.clientX - rect.left) / Math.max(1, rect.width), 0, 1);
    const edge = Math.min(PREVIEW_WIDTH / 2 + 8, hostRect.width / 2);
    const maxX = Math.max(edge, hostRect.width - edge);
    onPreviewHover({
      x: clamp(event.clientX - hostRect.left, edge, maxX),
      ratio,
      time: ratio * duration,
    });
  }

  function hide() {
    onPreviewHover(null);
  }

  progressEl.addEventListener("pointermove", update);
  progressEl.addEventListener("pointerdown", update);
  progressEl.addEventListener("pointerleave", hide);
  window.addEventListener("pointerup", hide);
  window.addEventListener("blur", hide);

  return () => {
    progressEl.removeEventListener("pointermove", update);
    progressEl.removeEventListener("pointerdown", update);
    progressEl.removeEventListener("pointerleave", hide);
    window.removeEventListener("pointerup", hide);
    window.removeEventListener("blur", hide);
  };
}

function maybeOfferResume(
  videoKey: string,
  video: HTMLVideoElement,
  onResumeAvailable: (prompt: ResumePrompt | null) => void
) {
  const record = readPlaybackRecord(videoKey);
  const duration = video.duration;
  if (
    !record ||
    !Number.isFinite(duration) ||
    duration <= 0 ||
    record.time < RESUME_MIN_SECONDS ||
    record.time > duration - RESUME_END_GAP_SECONDS
  ) {
    onResumeAvailable(null);
    return;
  }
  onResumeAvailable({ time: record.time });
}

function savePlaybackRecord(videoKey: string, video: HTMLVideoElement) {
  const duration = video.duration;
  const time = video.currentTime;
  if (!Number.isFinite(duration) || duration <= 0 || !Number.isFinite(time)) {
    return;
  }
  if (time > duration - RESUME_END_GAP_SECONDS) {
    clearPlaybackRecord(videoKey);
    return;
  }
  if (time < RESUME_MIN_SECONDS) return;

  const key = playbackStorageKey(videoKey);
  const previous = readPlaybackRecord(videoKey);
  if (previous && Math.abs(previous.time - time) < 2) return;
  safeSetJSON(key, { time, duration, updatedAt: Date.now() });
}

function readPlaybackRecord(videoKey: string): PlaybackRecord | null {
  const value = safeGetJSON<PlaybackRecord>(playbackStorageKey(videoKey));
  if (!value || Date.now() - value.updatedAt > 1000 * 60 * 60 * 24 * 30) {
    return null;
  }
  return value;
}

function clearPlaybackRecord(videoKey: string) {
  try {
    localStorage.removeItem(playbackStorageKey(videoKey));
  } catch {
    // ignore
  }
}

function playbackStorageKey(videoKey: string) {
  return PLAYBACK_KEY_PREFIX + encodeURIComponent(videoKey);
}

function readPlayerSettings(): PlayerSettings {
  const saved = safeGetJSON<Partial<PlayerSettings>>(SETTINGS_KEY) ?? {};
  return {
    volume: clampNumber(saved.volume, DEFAULT_SETTINGS.volume, 0, 1),
    muted: typeof saved.muted === "boolean" ? saved.muted : DEFAULT_SETTINGS.muted,
    playbackRate: clampNumber(saved.playbackRate, DEFAULT_SETTINGS.playbackRate, 0.5, 3),
  };
}

function writePlayerSettings(patch: Partial<PlayerSettings>) {
  safeSetJSON(SETTINGS_KEY, { ...readPlayerSettings(), ...patch });
}

function mediaErrorMessage(error: MediaError | null) {
  switch (error?.code) {
    case MediaError.MEDIA_ERR_ABORTED:
      return "视频加载已取消，请重试。";
    case MediaError.MEDIA_ERR_NETWORK:
      return "视频源网络连接失败，请稍后重试。";
    case MediaError.MEDIA_ERR_DECODE:
      return "视频编码无法解码，可能需要转码或换用浏览器。";
    case MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED:
      return "视频源暂不可用或格式不受当前浏览器支持。";
    default:
      return "视频源暂时无法播放，请重试或复制地址排查。";
  }
}

function syncPreviewVideo(video: HTMLVideoElement | null, ratio: number) {
  if (!video || !Number.isFinite(video.duration) || video.duration <= 0) return;
  const target = clamp(ratio * video.duration, 0, Math.max(0, video.duration - 0.05));
  if (Math.abs(video.currentTime - target) > 0.25) {
    try {
      video.currentTime = target;
    } catch {
      // ignore
    }
  }
}

function showTransientHud(
  setGestureHud: (hud: GestureHud | null) => void,
  label: string
) {
  const key = Date.now();
  setGestureHud({ key, label });
  window.setTimeout(() => {
    setGestureHud(null);
  }, 900);
}

function fallbackCopyText(text: string) {
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.select();
  try {
    document.execCommand("copy");
  } catch {
    // ignore
  } finally {
    textarea.remove();
  }
}

function safeGetJSON<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : null;
  } catch {
    return null;
  }
}

function safeSetJSON(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // ignore
  }
}

function clampNumber(
  value: unknown,
  fallback: number,
  min: number,
  max: number
) {
  return typeof value === "number" && Number.isFinite(value)
    ? clamp(value, min, max)
    : fallback;
}

function clamp(n: number, min: number, max: number) {
  return n < min ? min : n > max ? max : n;
}

function formatClock(seconds: number) {
  if (!Number.isFinite(seconds) || seconds < 0) return "00:00";
  const total = Math.floor(seconds);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) {
    return `${String(h).padStart(2, "0")}:${String(m).padStart(
      2,
      "0"
    )}:${String(s).padStart(2, "0")}`;
  }
  return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}
