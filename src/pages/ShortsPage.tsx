import { memo, useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import {
  ChevronLeft,
  Heart,
  Maximize,
  Minimize,
  Volume2,
  VolumeX,
  EyeOff,
  Info,
  Sparkles,
  AlertCircle,
} from "lucide-react";
import {
  fetchShortsNext,
  hideVideo,
  type ShortsItem,
} from "@/data/videos";
import "@/styles/shorts.css";

// 短视频"已看过"列表存在 localStorage，与普通详情页历史完全独立。
const SEEN_STORAGE_KEY = "shorts_seen_ids_v1";

// 每次向后端取多少条续到队列尾。值不要太大避免一次返回过多浪费；
// 也不要太小导致频繁请求和滑动卡顿。
const BATCH_SIZE = 5;

// 当队列里"还没看过的视频"少于这个数时，提前请求下一批。
const PREFETCH_THRESHOLD = 2;

// 当前视频至少有这么多秒的前向缓冲后，才允许后续视频开始预加载。
const ACTIVE_PRELOAD_BUFFER_SECONDS = 12;

// 当前视频流畅播放后，向后预加载多少条视频。
const PRELOAD_AHEAD_COUNT = 2;

// 预加载授权一旦发出，只有当前视频前向缓冲跌破这个秒数（或发生 stall）
// 才收回。高低水位之间不动作，避免缓冲量在 12s 附近波动时
// 反复绑定/剥离后续视频的 src、丢弃已预加载的数据。
const ACTIVE_PRELOAD_KEEP_SECONDS = 4;

// 维护一个固定大小的视频窗口：窗口内才 mount 真实 <video> 壳。
// 当前屏先绑定 src；后续预加载要等当前屏缓冲健康后才开始。
// 窗口内只要已经产生过可复用缓冲，就保留 src 复用浏览器缓存。
const VIDEO_WINDOW_SIZE = 6;

function loadSeenIds(): string[] {
  try {
    const raw = localStorage.getItem(SEEN_STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((x): x is string => typeof x === "string");
  } catch {
    return [];
  }
}

function saveSeenIds(ids: string[]) {
  try {
    localStorage.setItem(SEEN_STORAGE_KEY, JSON.stringify(ids));
  } catch {
    // 配额满或隐私模式：忽略，最多导致下一轮可能重复，不影响功能
  }
}

export default function ShortsPage() {
  // 已加入页面的视频队列（按出现顺序）
  const [items, setItems] = useState<ShortsItem[]>([]);
  // 当前在视口里的视频索引
  const [activeIndex, setActiveIndex] = useState(0);
  // 是否静音；首次必须静音才能 autoplay，用户点击后切换
  const [muted, setMuted] = useState(true);
  // 音量大小 (0 ~ 1)
  const [volume, setVolume] = useState(0.8);
  // 全局 Toast / HUD 提醒文字
  const [hudText, setHudText] = useState<{ id: number; text: string; icon?: React.ReactNode } | null>(null);
  const hudTimeoutRef = useRef<number | null>(null);

  const showHud = useCallback((text: string, icon?: React.ReactNode) => {
    if (hudTimeoutRef.current) window.clearTimeout(hudTimeoutRef.current);
    setHudText({ id: Date.now(), text, icon });
    hudTimeoutRef.current = window.setTimeout(() => {
      setHudText(null);
    }, 1500);
  }, []);

  const stopHeaderControlPropagation = useCallback((e: React.SyntheticEvent) => {
    e.stopPropagation();
  }, []);

  const handleVolumeButtonClick = useCallback(() => {
    const activeVideo = videoRefs.current.get(activeIndex);
    const canResumeActiveVideo = () =>
      Boolean(activeVideo) &&
      videoRefs.current.get(activeIndexRef.current) === activeVideo &&
      userPausedIndexRef.current !== activeIndexRef.current;
    const wasPlaying = Boolean(activeVideo) && canResumeActiveVideo() && !activeVideo?.paused;
    setMuted((v) => {
      const next = !v;
      if (activeVideo) {
        normalizeVideoPlaybackRate(activeVideo);
        applyVideoAudioState(activeVideo, next, volume);
        stabilizeVideoAfterAudioToggle(
          activeVideo,
          () => wasPlaying && canResumeActiveVideo()
        );
      }
      showHud(
        next ? "已静音" : "音量已开启",
        next ? <VolumeX size={16} /> : <Volume2 size={16} />
      );
      return next;
    });
  }, [activeIndex, showHud, volume]);

  const handleVolumeSliderChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const val = parseFloat(e.target.value);
    setVolume(val);
    if (val > 0) {
      setMuted(false);
    } else {
      setMuted(true);
    }
    // Update active video volume directly
    const activeVideo = videoRefs.current.get(activeIndex);
    if (activeVideo) {
      normalizeVideoPlaybackRate(activeVideo);
      applyVideoAudioState(activeVideo, val === 0, val);
      const wasPlaying =
        videoRefs.current.get(activeIndexRef.current) === activeVideo &&
        userPausedIndexRef.current !== activeIndexRef.current &&
        !activeVideo.paused;
      stabilizeVideoAfterAudioToggle(
        activeVideo,
        () =>
          wasPlaying &&
          videoRefs.current.get(activeIndexRef.current) === activeVideo &&
          userPausedIndexRef.current !== activeIndexRef.current
      );
    }
  }, [activeIndex]);

  // 组件卸载时清理 HUD 定时器
  useEffect(() => {
    return () => {
      if (hudTimeoutRef.current) window.clearTimeout(hudTimeoutRef.current);
    };
  }, []);

  // 是否正在加载下一批，避免并发请求
  const [loading, setLoading] = useState(false);
  // 后端报告"本轮已耗尽"，下次请求前会自动重置
  const [roundComplete, setRoundComplete] = useState(false);
  // 没有任何视频可放（库为空 / 全部隐藏）
  const [empty, setEmpty] = useState(false);

  // seenIds 用 ref 维护，方便在异步 callback 里读到最新值
  const seenIdsRef = useRef<string[]>(loadSeenIds());

  const containerRef = useRef<HTMLDivElement | null>(null);
  // 整个页面根元素，用于 requestFullscreen
  const pageRef = useRef<HTMLDivElement | null>(null);
  // index → video element，用来精确控制播放/暂停
  const videoRefs = useRef<Map<number, HTMLVideoElement>>(new Map());
  const activeIndexRef = useRef(0);
  const userPausedIndexRef = useRef<number | null>(null);
  const ignoreIntersectionUntilRef = useRef(0);
  const fullscreenRestoreTimersRef = useRef<number[]>([]);
  const fullscreenPointerHandledRef = useRef(false);
  const [activeReadyForPreload, setActiveReadyForPreload] = useState(false);
  const [userPausedIndex, setUserPausedIndexState] = useState<number | null>(null);
  const [cacheableSourceIds, setCacheableSourceIds] = useState<Set<string>>(
    () => new Set()
  );
  const [cacheWindowHighIndex, setCacheWindowHighIndex] = useState(-1);

  // 当前是否处在浏览器全屏（Fullscreen API）状态。
  // iPhone Safari 不支持网页元素级全屏；那种环境下改用页面滚动让浏览器栏随刷动收起。
  const useDocumentScroll = shouldUseDocumentScrollForShorts();
  const [canRequestFullscreen, setCanRequestFullscreen] = useState(() =>
    supportsElementFullscreenAPI()
  );
  const [isFullscreen, setIsFullscreen] = useState(false);

  // 本次会话内已经点过赞的视频 id 集合。
  // 与后端的真实 likes 字段同步——后端是单纯计数器，前端在这里防重避免连发。
  // 用户在操作栏点取消时会从这里移除，允许之后再次点赞。
  const likedIdsRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    activeIndexRef.current = activeIndex;
  }, [activeIndex]);

  useEffect(() => {
    const page = pageRef.current;
    if (page && supportsElementFullscreenAPI(page)) {
      setCanRequestFullscreen(true);
    }
  }, []);

  const updateUserPausedIndex = useCallback((index: number | null) => {
    userPausedIndexRef.current = index;
    setUserPausedIndexState(index);
  }, []);

  const setUserPausedForIndex = useCallback(
    (index: number, isPaused: boolean) => {
      if (isPaused) {
        updateUserPausedIndex(index);
      } else if (userPausedIndexRef.current === index) {
        updateUserPausedIndex(null);
      }
    },
    [updateUserPausedIndex]
  );

  const isVideoPausedByUser = useCallback(
    (index: number) => userPausedIndexRef.current === index,
    []
  );

  useEffect(() => {
    updateUserPausedIndex(null);
  }, [activeIndex, updateUserPausedIndex]);

  const handleActiveReadyForPreload = useCallback((index: number) => {
    if (index === activeIndexRef.current) {
      setActiveReadyForPreload(true);
    }
  }, []);

  const handleActiveNeedsPriority = useCallback((index: number) => {
    if (index === activeIndexRef.current) {
      setActiveReadyForPreload(false);
    }
  }, []);

  // 标记某条视频"浏览器里已有可复用的缓冲"。之后只要它还在缓存窗口内，
  // 就保留 src 不剥离，回滑/再前滑时直接续用已缓冲数据，秒开不卡顿。
  const handleSourceCached = useCallback((videoId: string) => {
    setCacheableSourceIds((prev) => {
      if (prev.has(videoId)) return prev;
      const next = new Set(prev);
      next.add(videoId);
      return next;
    });
  }, []);

  /**
   * 切换点赞状态。
   * - liked=true：发 POST /api/video/:id/like
   * - liked=false：发 DELETE /api/video/:id/like
   * 返回服务端最新 likes 值；请求失败返回 null（调用方可回滚 UI）。
   */
  const handleLikeToggle = useCallback(
    async (videoId: string, liked: boolean): Promise<number | null> => {
      // 维护本地集合以保持双击去重逻辑（已经在集合里就不会重复点赞）
      if (liked) {
        likedIdsRef.current.add(videoId);
      } else {
        likedIdsRef.current.delete(videoId);
      }
      try {
        const res = await fetch(
          `/api/video/${encodeURIComponent(videoId)}/like`,
          {
            method: liked ? "POST" : "DELETE",
            credentials: "include",
          }
        );
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = (await res.json()) as { likes?: number };
        return typeof data.likes === "number" ? data.likes : null;
      } catch {
        // 请求失败：回滚集合，让 Slide 自己回滚 UI
        if (liked) {
          likedIdsRef.current.delete(videoId);
        } else {
          likedIdsRef.current.add(videoId);
        }
        return null;
      }
    },
    []
  );

  /** 当前 id 是否已经在本次会话内点过赞（供 Slide 切换 active 时同步状态） */
  const hasLiked = useCallback(
    (videoId: string) => likedIdsRef.current.has(videoId),
    []
  );

  /**
   * 向后端请求下一批不重复的短视频，追加到 items 末尾。
   */
  const loadMore = useCallback(async () => {
    if (loading) return;
    setLoading(true);
    try {
      const seen = seenIdsRef.current;
      const resp = await fetchShortsNext(seen, BATCH_SIZE);
      if (resp.items.length === 0) {
        setEmpty((prev) => prev || true /* 维持 true 即可 */);
        setRoundComplete(true);
        return;
      }
      setEmpty(false);
      setItems((prev) => {
        const existing = new Set(prev.map((v) => v.id));
        const fresh = resp.items.filter((v) => !existing.has(v.id));
        return [...prev, ...fresh];
      });
      setRoundComplete(resp.roundComplete);
    } finally {
      setLoading(false);
    }
  }, [loading]);

  // 首次加载
  useEffect(() => {
    void loadMore();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 仅当 items 还是空时，把 empty 设回 false 是没必要的；上面 loadMore 已处理
  useEffect(() => {
    if (items.length > 0) setEmpty(false);
  }, [items.length]);

  // 把当前活跃视频的 id 写入已看列表，并在剩余不足时续取
  useEffect(() => {
    const active = items[activeIndex];
    if (!active) return;

    setCacheWindowHighIndex((prev) => Math.max(prev, activeIndex));

    if (!seenIdsRef.current.includes(active.id)) {
      seenIdsRef.current = [...seenIdsRef.current, active.id];
      saveSeenIds(seenIdsRef.current);
    }

    const remaining = items.length - 1 - activeIndex;
    if (remaining < PREFETCH_THRESHOLD && !loading) {
      if (roundComplete) {
        // 上一次后端说"本轮已耗尽"时，必须等用户真正滑到当前队列最后一条
        // 再清空已看记录开新一轮。否则退出后重新进入会把未完成轮次提前重置，
        // 导致刚刷过的视频再次出现在下一次会话里。
        if (remaining > 0) return;
        seenIdsRef.current = [];
        saveSeenIds([]);
        setRoundComplete(false);
      }
      void loadMore();
    }
  }, [activeIndex, items, loading, roundComplete, loadMore]);

  // 用 IntersectionObserver 找出当前进入视口的 item。
  // root 直接用 viewport：普通模式和 iPhone 页面滚动模式都能正确观测。
  useEffect(() => {
    const root = containerRef.current;
    if (!root) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (Date.now() < ignoreIntersectionUntilRef.current) return;

        let bestIndex = -1;
        let bestRatio = 0.6;
        for (const entry of entries) {
          if (entry.intersectionRatio > bestRatio) {
            bestRatio = entry.intersectionRatio;
            const idx = Number(
              (entry.target as HTMLElement).dataset.index ?? -1
            );
            if (!Number.isNaN(idx)) bestIndex = idx;
          }
        }
        if (bestIndex >= 0 && bestIndex !== activeIndexRef.current) {
          activeIndexRef.current = bestIndex;
          setActiveReadyForPreload(false);
          setActiveIndex(bestIndex);
        }
      },
      {
        root: null,
        threshold: [0.6, 0.85],
      }
    );

    const slides = root.querySelectorAll<HTMLElement>("[data-shorts-slide]");
    slides.forEach((el) => observer.observe(el));
    return () => observer.disconnect();
  }, [items.length]);

  // 控制每个 video 的播放状态：只有 activeIndex 对应的在播。
  // 声音切换不要进入这里，否则移动端切换 muted 时可能额外触发 play/pause。
  useEffect(() => {
    videoRefs.current.forEach((video, idx) => {
      if (idx === activeIndex) {
        if (userPausedIndex === idx) {
          if (!video.paused) video.pause();
        } else if (video.paused) {
          video.play().catch(() => undefined);
        }
      } else {
        if (!video.paused) video.pause();
      }
    });
  }, [activeIndex, items.length, userPausedIndex]);

  // 单独同步音频属性。这里不做 play/pause，避免手机端切换静音时打断播放节奏。
  useEffect(() => {
    videoRefs.current.forEach((video) => {
      applyVideoAudioState(video, muted, volume);
    });
  }, [muted, volume, items.length]);

  // 键盘快捷键监听
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const activeEl = document.activeElement;
      if (activeEl && (activeEl.tagName === "INPUT" || activeEl.tagName === "TEXTAREA")) {
        return;
      }

      if (e.key === "ArrowDown") {
        e.preventDefault();
        const nextIdx = activeIndex + 1;
        if (nextIdx < items.length) {
          const nextSlide = containerRef.current?.querySelector(`[data-index="${nextIdx}"]`);
          if (nextSlide) {
            nextSlide.scrollIntoView({ behavior: "smooth" });
          }
        }
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        const prevIdx = activeIndex - 1;
        if (prevIdx >= 0) {
          const prevSlide = containerRef.current?.querySelector(`[data-index="${prevIdx}"]`);
          if (prevSlide) {
            prevSlide.scrollIntoView({ behavior: "smooth" });
          }
        }
      } else if (e.key === " ") {
        e.preventDefault();
        const activeVideo = videoRefs.current.get(activeIndex);
        if (activeVideo) {
          const shouldResume =
            userPausedIndexRef.current === activeIndex ||
            (activeVideo.paused && activeVideo.readyState >= 3);
          if (shouldResume) {
            setUserPausedForIndex(activeIndex, false);
            activeVideo.play().catch(() => undefined);
          } else {
            setUserPausedForIndex(activeIndex, true);
            activeVideo.pause();
          }
        }
      } else if (e.key === "m" || e.key === "M") {
        e.preventDefault();
        handleVolumeButtonClick();
      } else if (e.key === "f" || e.key === "F") {
        e.preventDefault();
        toggleFullscreen();
      } else if (e.key === "l" || e.key === "L") {
        e.preventDefault();
        const heartBtn = containerRef.current?.querySelector(`[data-index="${activeIndex}"] .shorts-slide__action`) as HTMLButtonElement | null;
        if (heartBtn) {
          heartBtn.click();
        }
      } else if (e.key === "ArrowRight") {
        e.preventDefault();
        const activeVideo = videoRefs.current.get(activeIndex);
        if (activeVideo && activeVideo.duration) {
          const newTime = Math.min(activeVideo.duration, activeVideo.currentTime + 5);
          activeVideo.currentTime = newTime;
          showHud("+5秒", <Sparkles size={16} />);
        }
      } else if (e.key === "ArrowLeft") {
        e.preventDefault();
        const activeVideo = videoRefs.current.get(activeIndex);
        if (activeVideo && activeVideo.duration) {
          const newTime = Math.max(0, activeVideo.currentTime - 5);
          activeVideo.currentTime = newTime;
          showHud("-5秒", <Sparkles size={16} />);
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [activeIndex, items, toggleFullscreen, showHud, handleVolumeButtonClick, setUserPausedForIndex]);

  // 页面卸载时暂停所有
  useEffect(() => {
    return () => {
      videoRefs.current.forEach((v) => {
        try {
          v.pause();
        } catch {
          // ignore
        }
      });
    };
  }, []);

  // 为每个 index 缓存一个稳定的 ref 回调函数，避免每次 render 都生成新闭包，
  // 导致子组件 ShortsSlide 即使被 memo 包了也会因 props 变化重渲染。
  const setVideoRefMapRef = useRef<Map<number, (el: HTMLVideoElement | null) => void>>(new Map());
  const setVideoRef = useCallback((index: number) => {
    let cached = setVideoRefMapRef.current.get(index);
    if (!cached) {
      cached = (el: HTMLVideoElement | null) => {
        if (el) videoRefs.current.set(index, el);
        else videoRefs.current.delete(index);
      };
      setVideoRefMapRef.current.set(index, cached);
    }
    return cached;
  }, []);

  useEffect(() => {
    document.title = "短视频 · 61";
  }, []);

  // 沉浸式：默认锁住 body 滚动；iPhone 浏览器里放开根页面滚动，让 Safari 工具栏能随刷动收起。
  useEffect(() => {
    const html = document.documentElement;
    const body = document.body;
    const prevHtmlOverflow = html.style.overflow;
    const prevBodyOverflow = body.style.overflow;
    const prevBodyBg = body.style.background;
    if (useDocumentScroll) {
      html.classList.add("shorts-document-scroll");
      body.classList.add("shorts-document-scroll");
    } else {
      html.style.overflow = "hidden";
      body.style.overflow = "hidden";
      body.style.background = "#000";
    }

    let prevThemeColor: string | null = null;
    let themeMeta = document.querySelector<HTMLMetaElement>(
      'meta[name="theme-color"]'
    );
    const createdMeta = !themeMeta;
    if (!themeMeta) {
      themeMeta = document.createElement("meta");
      themeMeta.name = "theme-color";
      document.head.appendChild(themeMeta);
    } else {
      prevThemeColor = themeMeta.content;
    }
    themeMeta.content = "#000000";

    return () => {
      html.classList.remove("shorts-document-scroll");
      body.classList.remove("shorts-document-scroll");
      html.style.overflow = prevHtmlOverflow;
      body.style.overflow = prevBodyOverflow;
      body.style.background = prevBodyBg;
      if (themeMeta) {
        if (createdMeta) {
          themeMeta.remove();
        } else if (prevThemeColor !== null) {
          themeMeta.content = prevThemeColor;
        }
      }
    };
  }, [useDocumentScroll]);

  function clearFullscreenRestoreTimers() {
    for (const timer of fullscreenRestoreTimersRef.current) {
      window.clearTimeout(timer);
    }
    fullscreenRestoreTimersRef.current = [];
  }

  function restoreActiveSlideIntoView() {
    const idx = activeIndexRef.current;
    const slide = containerRef.current?.querySelector<HTMLElement>(
      `[data-index="${idx}"]`
    );
    if (!slide) return;
    slide.scrollIntoView({ block: "start", inline: "nearest", behavior: "auto" });
  }

  function scheduleFullscreenActiveRestore() {
    ignoreIntersectionUntilRef.current = Date.now() + 700;
    clearFullscreenRestoreTimers();
    restoreActiveSlideIntoView();
    fullscreenRestoreTimersRef.current = [80, 220, 520].map((delay) =>
      window.setTimeout(restoreActiveSlideIntoView, delay)
    );
  }

  // ---- 浏览器全屏（Fullscreen API） ----
  // 监听全屏状态变化，保持 React state 同步。
  // 用户按 ESC / 系统返回 / 浏览器退出全屏按钮 时也会走这里。
  useEffect(() => {
    function handleChange() {
      scheduleFullscreenActiveRestore();
      setIsFullscreen(
        document.fullscreenElement !== null ||
          // Safari (desktop) 旧前缀
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          (document as any).webkitFullscreenElement != null
      );
    }
    document.addEventListener("fullscreenchange", handleChange);
    document.addEventListener("webkitfullscreenchange", handleChange);
    return () => {
      document.removeEventListener("fullscreenchange", handleChange);
      document.removeEventListener("webkitfullscreenchange", handleChange);
      clearFullscreenRestoreTimers();
    };
  }, []);

  // 路由离开 / 组件卸载时主动退出全屏，避免残留全屏态
  useEffect(() => {
    return () => {
      try {
        if (document.fullscreenElement) {
          void document.exitFullscreen();
        }
      } catch {
        // ignore
      }
    };
  }, []);

  function requestPageFullscreen() {
    if (!canRequestFullscreen) return;
    const page = pageRef.current;
    if (!page) return;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const anyPage = page as any;
    const fn: (() => Promise<void>) | undefined =
      page.requestFullscreen?.bind(page) ||
      anyPage.webkitRequestFullscreen?.bind(page);
    if (!fn) return;
    try {
      const ret = fn();
      if (ret && typeof ret.then === "function") {
        ret.catch(() => {
          // iOS Safari 或被拒绝：静默忽略，沉浸样式仍然生效
        });
      }
    } catch {
      // ignore
    }
  }

  function exitPageFullscreen() {
    try {
      if (document.exitFullscreen) {
        void document.exitFullscreen();
      } else {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const anyDoc = document as any;
        if (typeof anyDoc.webkitExitFullscreen === "function") {
          anyDoc.webkitExitFullscreen();
        }
      }
    } catch {
      // ignore
    }
  }

  function toggleFullscreen() {
    scheduleFullscreenActiveRestore();
    if (canRequestFullscreen) {
      if (isFullscreen) exitPageFullscreen();
      else requestPageFullscreen();
      return;
    }
    if (useDocumentScroll) {
      restoreActiveSlideIntoView();
    }
  }

  function handleFullscreenButtonPointerDown(
    e: React.PointerEvent<HTMLButtonElement>
  ) {
    e.preventDefault();
    e.stopPropagation();
    fullscreenPointerHandledRef.current = true;
    toggleFullscreen();
  }

  function handleFullscreenButtonClick(e: React.MouseEvent<HTMLButtonElement>) {
    e.preventDefault();
    e.stopPropagation();
    if (fullscreenPointerHandledRef.current) {
      fullscreenPointerHandledRef.current = false;
      return;
    }
    toggleFullscreen();
  }

  const handleHideSuccess = useCallback((idx: number) => {
    showHud("已选择不再展示，正在滑至下一首...", <EyeOff size={16} />);
    const nextIdx = idx + 1;
    if (nextIdx < items.length) {
      setTimeout(() => {
        const nextSlide = containerRef.current?.querySelector(`[data-index="${nextIdx}"]`);
        if (nextSlide) {
          nextSlide.scrollIntoView({ behavior: "smooth" });
        }
      }, 700);
    }
  }, [items.length, showHud]);

  const videoWindow = getVideoWindowBounds(cacheWindowHighIndex, items.length);

  return (
    <div
      className={`shorts-page${useDocumentScroll ? " is-document-scroll" : ""}`}
      ref={pageRef}
    >
      <header className="shorts-header">
        <Link to="/" className="shorts-header__back" aria-label="返回首页">
          <ChevronLeft size={22} />
        </Link>
        <div className="shorts-header__actions">
          <button
            type="button"
            className="shorts-header__icon-btn"
            aria-label={isFullscreen ? "退出全屏" : "进入全屏"}
            onPointerDown={handleFullscreenButtonPointerDown}
            onClick={handleFullscreenButtonClick}
          >
            {isFullscreen ? <Minimize size={20} /> : <Maximize size={20} />}
          </button>
          
          <div className="shorts-header__volume-group">
            <div className="shorts-header__volume-slider-container">
              <input
                type="range"
                min="0"
                max="1"
                step="0.05"
                value={muted ? 0 : volume}
                onChange={handleVolumeSliderChange}
                className="shorts-header__volume-slider"
                aria-label="音量调节"
              />
            </div>
            <button
              type="button"
              className="shorts-header__icon-btn"
              aria-label={muted ? "取消静音" : "静音"}
              onPointerDownCapture={stopHeaderControlPropagation}
              onTouchStartCapture={stopHeaderControlPropagation}
              onMouseDownCapture={stopHeaderControlPropagation}
              onPointerDown={stopHeaderControlPropagation}
              onTouchStart={stopHeaderControlPropagation}
              onMouseDown={stopHeaderControlPropagation}
              onClick={(e) => {
                e.stopPropagation();
                handleVolumeButtonClick();
              }}
            >
              {muted || volume === 0 ? <VolumeX size={20} /> : <Volume2 size={20} />}
            </button>
          </div>
        </div>
      </header>

      {hudText && (
        <div key={hudText.id} className="shorts-hud-toast">
          {hudText.icon}
          <span>{hudText.text}</span>
        </div>
      )}

      <div className="shorts-feed" ref={containerRef}>
        {empty && (
          <div className="shorts-empty">
            <p>当前没有可播放的视频</p>
            <Link to="/" className="shorts-empty__link">
              返回首页
            </Link>
          </div>
        )}

        {items.map((item, index) => {
          const isActiveSlide = index === activeIndex;
          const isInCacheWindow =
            index >= videoWindow.start && index <= videoWindow.end;
          const preloadOffset = index - activeIndex;
          const shouldPreload =
            activeReadyForPreload &&
            preloadOffset > 0 &&
            preloadOffset <= PRELOAD_AHEAD_COUNT;
          const shouldMount = isActiveSlide || isInCacheWindow || shouldPreload;
          // 视频窗口内已经缓冲过的视频保留 src：
          // 在窗口内来回切换时，直接复用浏览器已缓冲数据。
          const shouldRetainCached =
            isInCacheWindow && !isActiveSlide && cacheableSourceIds.has(item.id);
          const shouldLoad = isActiveSlide || shouldPreload || shouldRetainCached;
          const shouldEagerLoad = isActiveSlide || shouldPreload;
          return (
            <ShortsSlide
              key={item.id}
              item={item}
              index={index}
              isActive={isActiveSlide}
              // 固定 6 条视频窗口内才挂载 <video> 壳；
              // 当前屏先绑定 src；后两个视频等当前屏缓冲健康后再预加载；
              // 已缓冲过的窗口内视频保留 src，便于来回切换复用缓存。
              shouldMount={shouldMount}
              shouldLoad={shouldLoad}
              shouldEagerLoad={shouldEagerLoad}
              muted={muted}
              volume={volume}
              setMuted={setMuted}
              setVolume={setVolume}
              videoRef={setVideoRef(index)}
              onLikeToggle={handleLikeToggle}
              hasLiked={hasLiked}
              onHideSuccess={handleHideSuccess}
              onActiveReadyForPreload={handleActiveReadyForPreload}
              onActiveNeedsPriority={handleActiveNeedsPriority}
              onSourceCached={handleSourceCached}
              onUserPausedChange={setUserPausedForIndex}
              isVideoPausedByUser={isVideoPausedByUser}
              showHud={showHud}
            />
          );
        })}
      </div>
    </div>
  );
}

type SlideProps = {
  item: ShortsItem;
  index: number;
  isActive: boolean;
  shouldMount: boolean;
  shouldLoad: boolean;
  shouldEagerLoad: boolean;
  muted: boolean;
  volume: number;
  setMuted: (muted: boolean) => void;
  setVolume: (volume: number) => void;
  videoRef: (el: HTMLVideoElement | null) => void;
  /**
   * 切换点赞。第二参数 true 表示点赞，false 表示取消。
   * 返回服务端最新 likes 值；null 表示请求失败，调用方应回滚 UI。
   */
  onLikeToggle: (videoId: string, liked: boolean) => Promise<number | null>;
  /** 父组件查询某 id 是否已经在本次会话内点过赞 */
  hasLiked: (videoId: string) => boolean;
  onHideSuccess: (index: number) => void;
  onActiveReadyForPreload: (index: number) => void;
  onActiveNeedsPriority: (index: number) => void;
  /** 本条视频在浏览器里已有可复用缓冲，之后在视频窗口内保留 src */
  onSourceCached: (videoId: string) => void;
  onUserPausedChange: (index: number, isPaused: boolean) => void;
  isVideoPausedByUser: (index: number) => boolean;
  showHud: (text: string, icon?: React.ReactNode) => void;
};

/**
 * 一屏短视频。
 *
 * - 长按 ≥400ms 进入 2 倍速，松手恢复（与详情页 VideoPlayer 行为一致）
 * - 单击切换播放 / 暂停
 * - 长按弹出的下载/分享菜单通过 contextmenu + CSS 屏蔽
 */
function ShortsSlideInner({
  item,
  index,
  isActive,
  shouldMount,
  shouldLoad,
  shouldEagerLoad,
  muted,
  volume,
  setMuted,
  setVolume,
  videoRef,
  onLikeToggle,
  hasLiked,
  onHideSuccess,
  onActiveReadyForPreload,
  onActiveNeedsPriority,
  onSourceCached,
  onUserPausedChange,
  isVideoPausedByUser,
  showHud,
}: SlideProps) {
  const localRef = useRef<HTMLVideoElement | null>(null);
  const [paused, setPaused] = useState(false);
  const [fastActive, setFastActive] = useState(false);

  // 视频缓冲状态
  const [isBuffering, setIsBuffering] = useState(false);
  // 是否已经被隐藏/拉黑
  const [isMarkedHidden, setIsMarkedHidden] = useState(false);

  // 进度状态。播放时由 timeupdate 更新；拖动时由用户输入更新
  const [duration, setDuration] = useState(0);
  const [currentTime, setCurrentTime] = useState(0);
  const [scrubbing, setScrubbing] = useState(false);
  const scrubbingRef = useRef(false);
  // 拖动开始时是否在播：用于拖完后判断要不要 resume
  const wasPlayingRef = useRef(true);

  // 点赞数和"是否已点过赞"状态。
  // 初始 likes 取自后端返回的列表项；isLiked 仅控制视觉态，
  // 真正的防重在父组件 likedIdsRef 里，这里只信任父返回的回执。
  const [likes, setLikes] = useState(item.likes ?? 0);
  const [isLiked, setIsLiked] = useState(false);
  // 屏幕中央的心形飞起动画（双击点赞时显示）
  const [heartBurst, setHeartBurst] = useState<{
    key: number;
    x: number;
    y: number;
  } | null>(null);

  // 单击和双击的延迟分发：第一次点击挂在定时器里，
  // 300ms 内有第二次就当双击点赞，否则当单击 toggle play
  const clickTimerRef = useRef<number | null>(null);
  const lastClickAtRef = useRef(0);

  // 切换视频时把 likes 同步到新视频的初始值；
  // isLiked 取自父组件的全局集合，这样切走再切回 / 同一 id 重复出现仍能保持视觉态
  useEffect(() => {
    setLikes(item.likes ?? 0);
    setIsLiked(hasLiked(item.id));
  }, [item.id, item.likes, hasLiked]);

  const setRef = useCallback(
    (el: HTMLVideoElement | null) => {
      localRef.current = el;
      videoRef(el);
    },
    [videoRef]
  );

  // 非当前屏/后续预加载/视频窗口内缓存视频不保留媒体源，确保离开窗口后浏览器中止原始网盘流。
  useEffect(() => {
    if (shouldLoad) return;
    const video = localRef.current;
    if (!video) return;
    try {
      video.pause();
      video.removeAttribute("src");
      video.load();
    } catch {
      // ignore
    }
    setDuration(0);
    setCurrentTime(0);
    setIsBuffering(false);
  }, [shouldLoad, item.id]);

  // 离开活跃后清掉本地的暂停状态，避免回来时 UI 还显示着 paused
  useEffect(() => {
    if (!isActive) {
      setPaused(false);
      setScrubbing(false);
      scrubbingRef.current = false;
      setIsBuffering(false);
    }
  }, [isActive]);

  // Sync volume state directly
  useEffect(() => {
    const video = localRef.current;
    if (video && isActive) {
      applyVideoAudioState(video, muted, volume);
    }
  }, [muted, volume, isActive]);

  // 离开活跃或者被隐藏时暂停视频
  useEffect(() => {
    if (isMarkedHidden && localRef.current) {
      try {
        localRef.current.pause();
      } catch {
        // ignore
      }
    }
  }, [isMarkedHidden]);

  // 监听 video 的时长 / 进度 / 缓冲状态 / 音量物理键变化。
  // VIDEO_WINDOW_SIZE 会让窗口外的 slide 先以海报占位，之后才挂载 video 壳；
  // 只有 shouldLoad=true 的当前屏/后续预加载/缓存窗口视频会绑定 src，因此不会一次拉完整队列。
  // 因此这里必须跟随 shouldMount 重新绑定，否则后续视频没有 timeupdate 事件。
  useEffect(() => {
    if (!shouldMount) {
      setDuration(0);
      setCurrentTime(0);
      setIsBuffering(false);
      return;
    }
    const video = localRef.current;
    if (!video) return;
    const handleLoaded = () => {
      if (Number.isFinite(video.duration) && video.duration > 0) {
        setDuration(video.duration);
      } else {
        setDuration(0);
      }
      if (!scrubbingRef.current) setCurrentTime(video.currentTime || 0);
    };
    const handleTime = () => {
      // 拖动期间不要被 timeupdate 覆盖 UI
      if (!scrubbingRef.current) setCurrentTime(video.currentTime);
      syncActivePreloadReadiness(video);
    };
    const handleWaiting = () => {
      if (video.paused || isVideoPausedByUser(index)) {
        setIsBuffering(false);
        return;
      }
      setIsBuffering(true);
      if (isActive) onActiveNeedsPriority(index);
    };
    const handlePlayingOrCanPlay = () => {
      // 已经能解码播放，说明浏览器里有了值得复用的数据。
      if (shouldLoad) onSourceCached(item.id);
      if (isActive && isVideoPausedByUser(index)) {
        video.pause();
        setPaused(true);
        setIsBuffering(false);
        return;
      }
      setIsBuffering(false);
      syncActivePreloadReadiness(video);
    };
    const handleProgress = () => {
      syncActivePreloadReadiness(video);
      // 窗口内视频只要已经产生缓冲，就标记为可复用；
      // 之后预加载授权被收回时不再丢弃它的 src 和已缓冲数据。
      if (shouldLoad && videoHasBufferedData(video)) {
        onSourceCached(item.id);
      }
    };
    const handleVolumeChange = () => {
      if (!isActive) return;
      // 当检测到 video 自身的 mute 状态或 volume 改变时，同步更新 React 状态。
      // 这可以在移动端浏览器支持物理音量键调整时，自动反向取消静音并展示音量 HUD。
      if (video.muted !== muted) {
        setMuted(video.muted);
      }
      if (video.volume !== volume) {
        setVolume(video.volume);
      }
    };
    const handlePlay = () => {
      if (!isActive) return;
      if (isVideoPausedByUser(index)) {
        video.pause();
        setPaused(true);
        setIsBuffering(false);
        return;
      }
      setPaused(false);
    };
    const handlePause = () => {
      if (!isActive || video.ended) return;
      setPaused(true);
      setIsBuffering(false);
    };

    function syncActivePreloadReadiness(currentVideo: HTMLVideoElement) {
      if (!isActive) return;
      if (videoHasComfortableBuffer(currentVideo)) {
        onActiveReadyForPreload(index);
      } else if (videoBufferIsCritical(currentVideo)) {
        // 高低水位滞回：只有缓冲真正告急才收回预加载授权，
        // 在两个水位之间维持现状，避免阈值附近来回抖动。
        onActiveNeedsPriority(index);
      }
    }

    handleLoaded();
    handleTime();
    video.addEventListener("loadedmetadata", handleLoaded);
    video.addEventListener("durationchange", handleLoaded);
    video.addEventListener("timeupdate", handleTime);
    video.addEventListener("waiting", handleWaiting);
    video.addEventListener("playing", handlePlayingOrCanPlay);
    video.addEventListener("canplay", handlePlayingOrCanPlay);
    video.addEventListener("progress", handleProgress);
    video.addEventListener("volumechange", handleVolumeChange);
    video.addEventListener("play", handlePlay);
    video.addEventListener("pause", handlePause);

    // 挂载时如果已经在播放但是状态不到 ready 则置 buffering
    if (video.readyState < 3 && !video.paused) {
      setIsBuffering(true);
    }

    return () => {
      video.removeEventListener("loadedmetadata", handleLoaded);
      video.removeEventListener("durationchange", handleLoaded);
      video.removeEventListener("timeupdate", handleTime);
      video.removeEventListener("waiting", handleWaiting);
      video.removeEventListener("playing", handlePlayingOrCanPlay);
      video.removeEventListener("canplay", handlePlayingOrCanPlay);
      video.removeEventListener("progress", handleProgress);
      video.removeEventListener("volumechange", handleVolumeChange);
      video.removeEventListener("play", handlePlay);
      video.removeEventListener("pause", handlePause);
    };
  }, [shouldMount, shouldLoad, item.id, index, isActive, muted, volume, setMuted, setVolume, onActiveReadyForPreload, onActiveNeedsPriority, onSourceCached, isVideoPausedByUser]);

  // 长按 2 倍速：直接绑原生事件
  useEffect(() => {
    const video = localRef.current;
    if (!video) return;
    let timer: number | null = null;
    let active = false;

    const clearTimer = () => {
      if (timer !== null) {
        window.clearTimeout(timer);
        timer = null;
      }
    };
    const start = () => {
      if (video.paused || video.ended) return;
      clearTimer();
      timer = window.setTimeout(() => {
        timer = null;
        if (video.paused || video.ended) return;
        video.playbackRate = 2;
        active = true;
        setFastActive(true);
      }, 400);
    };
    const end = () => {
      clearTimer();
      if (active) {
        active = false;
        video.playbackRate = 1;
        setFastActive(false);
      }
    };

    const handleTouchStart = () => start();
    const handleMouseDown = (e: MouseEvent) => {
      if (e.button === 0) start();
    };

    video.addEventListener("touchstart", handleTouchStart, { passive: true });
    video.addEventListener("touchend", end);
    video.addEventListener("touchcancel", end);
    video.addEventListener("mousedown", handleMouseDown);
    video.addEventListener("mouseup", end);
    video.addEventListener("mouseleave", end);
    video.addEventListener("pause", end);
    video.addEventListener("ended", end);

    return () => {
      clearTimer();
      video.removeEventListener("touchstart", handleTouchStart);
      video.removeEventListener("touchend", end);
      video.removeEventListener("touchcancel", end);
      video.removeEventListener("mousedown", handleMouseDown);
      video.removeEventListener("mouseup", end);
      video.removeEventListener("mouseleave", end);
      video.removeEventListener("pause", end);
      video.removeEventListener("ended", end);
    };
  }, [shouldMount]);

  function togglePlayInternal() {
    const video = localRef.current;
    if (!video) return;
    const shouldResume =
      isVideoPausedByUser(index) || (video.paused && paused && !isBuffering);
    if (shouldResume) {
      onUserPausedChange(index, false);
      video.play().catch(() => undefined);
      setPaused(false);
      if (video.readyState < 3) setIsBuffering(true);
    } else {
      onUserPausedChange(index, true);
      video.pause();
      setPaused(true);
      setIsBuffering(false);
    }
  }

  function clearClickTimer() {
    if (clickTimerRef.current !== null) {
      window.clearTimeout(clickTimerRef.current);
      clickTimerRef.current = null;
    }
  }

  /**
   * 单击 / 双击分发：
   * - 第一次点击：挂一个 280ms 定时器，到时如果还没第二次点击就 toggle 播放
   * - 第二次点击（280ms 内）：清掉定时器，当作双击点赞，不切换播放状态
   */
  function handleSlideClick(e: React.MouseEvent<HTMLElement>) {
    // 隐藏状态下不处理点击
    if (isMarkedHidden) return;

    const now = Date.now();
    const delta = now - lastClickAtRef.current;
    lastClickAtRef.current = now;

    // 双击命中
    if (delta < 280 && clickTimerRef.current !== null) {
      clearClickTimer();
      // 在双击位置弹心形动画
      const rect = e.currentTarget.getBoundingClientRect();
      handleDoubleClickLike(e.clientX - rect.left, e.clientY - rect.top);
      return;
    }

    // 单击挂起，等是否有第二次
    clearClickTimer();
    clickTimerRef.current = window.setTimeout(() => {
      clickTimerRef.current = null;
      togglePlayInternal();
    }, 280);
  }

  // 组件卸载时清理定时器
  useEffect(() => {
    return () => clearClickTimer();
  }, []);

  function handleDoubleClickLike(x: number, y: number) {
    // 触发飞心动画（每次都给一个新 key 强制重启动画）
    setHeartBurst({ key: Date.now(), x, y });
    window.setTimeout(() => setHeartBurst(null), 700);

    // 双击只表达喜爱：已经点赞了就只播动画不取消，不重复发请求；
    // 真要取消请点右下角心形按钮
    if (isLiked) return;
    setIsLiked(true);
    setLikes((n) => n + 1);
    void onLikeToggle(item.id, true).then((serverLikes) => {
      if (serverLikes !== null) {
        setLikes(serverLikes);
      } else {
        // 请求失败：回滚视觉态
        setIsLiked(false);
        setLikes((n) => Math.max(0, n - 1));
      }
    });
  }

  /**
   * 点击右下角心形按钮：在"已点赞 / 未点赞"之间切换。
   */
  function handleHeartClick(e: React.MouseEvent<HTMLButtonElement>) {
    e.stopPropagation();
    const willLike = !isLiked;
    if (willLike) {
      // 视觉立即响应 + 飞心动画（让按钮位置发出心形）
      const slideRect = (
        e.currentTarget.closest(".shorts-slide") as HTMLElement | null
      )?.getBoundingClientRect();
      const btnRect = e.currentTarget.getBoundingClientRect();
      if (slideRect) {
        const x = btnRect.left + btnRect.width / 2 - slideRect.left;
        const y = btnRect.top + btnRect.height / 2 - slideRect.top;
        setHeartBurst({ key: Date.now(), x, y });
        window.setTimeout(() => setHeartBurst(null), 700);
      }
      setIsLiked(true);
      setLikes((n) => n + 1);
      void onLikeToggle(item.id, true).then((serverLikes) => {
        if (serverLikes !== null) {
          setLikes(serverLikes);
        } else {
          setIsLiked(false);
          setLikes((n) => Math.max(0, n - 1));
        }
      });
    } else {
      // 取消点赞：视觉立即响应，请求失败再回滚
      setIsLiked(false);
      setLikes((n) => Math.max(0, n - 1));
      void onLikeToggle(item.id, false).then((serverLikes) => {
        if (serverLikes !== null) {
          setLikes(serverLikes);
        } else {
          setIsLiked(true);
          setLikes((n) => n + 1);
        }
      });
    }
  }



  /**
   * 拉黑并隐藏视频
   */
  function handleHideClick(e: React.MouseEvent<HTMLButtonElement>) {
    e.stopPropagation();
    setIsMarkedHidden(true);
    void hideVideo(item.id)
      .then((res) => {
        if (res.ok) {
          onHideSuccess(index);
        } else {
          setIsMarkedHidden(false);
          showHud("操作失败，请重试", <AlertCircle size={16} />);
        }
      })
      .catch(() => {
        setIsMarkedHidden(false);
        showHud("网络请求出错", <AlertCircle size={16} />);
      });
  }

  // ---- 进度条拖动 ----
  // 触摸进度条时：暂停 → 跟随手指更新 currentTime → 松手 resume
  function handleProgressPointerDown(e: React.PointerEvent<HTMLDivElement>) {
    e.preventDefault();
    e.stopPropagation();
    const video = localRef.current;
    const seekDuration = getSeekDuration(video);
    if (!video || !seekDuration) return;
    try {
      (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    } catch {
      // ignore
    }
    wasPlayingRef.current = !video.paused;
    if (!video.paused) {
      try {
        video.pause();
      } catch {
        // ignore
      }
    }
    scrubbingRef.current = true;
    setScrubbing(true);
    applyProgressFromEvent(e, seekDuration);
  }
  function handleProgressPointerMove(e: React.PointerEvent<HTMLDivElement>) {
    if (!scrubbingRef.current) return;
    e.preventDefault();
    e.stopPropagation();
    applyProgressFromEvent(e);
  }
  function handleProgressPointerEnd(e: React.PointerEvent<HTMLDivElement>) {
    if (!scrubbingRef.current) return;
    e.preventDefault();
    e.stopPropagation();
    try {
      (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
    } catch {
      // ignore
    }
    const video = localRef.current;
    scrubbingRef.current = false;
    setScrubbing(false);
    if (video && wasPlayingRef.current) {
      video.play().catch(() => undefined);
    }
  }
  function getSeekDuration(video: HTMLVideoElement | null) {
    if (duration > 0) return duration;
    if (video && Number.isFinite(video.duration) && video.duration > 0) {
      setDuration(video.duration);
      return video.duration;
    }
    return 0;
  }
  function applyProgressFromEvent(
    e: React.PointerEvent<HTMLDivElement>,
    knownDuration?: number
  ) {
    const video = localRef.current;
    const seekDuration = knownDuration ?? getSeekDuration(video);
    if (!video || !seekDuration) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const ratio = clamp((e.clientX - rect.left) / rect.width, 0, 1);
    const next = ratio * seekDuration;
    setCurrentTime(next);
    try {
      video.currentTime = next;
    } catch {
      // ignore（部分 ready state 下设置会抛错）
    }
  }

  const progressRatio = duration > 0 ? clamp(currentTime / duration, 0, 1) : 0;

  return (
    <article
      className="shorts-slide"
      data-shorts-slide=""
      data-index={index}
      data-active={isActive}
      onClick={handleSlideClick}
    >
      {/* 模糊海报背景：避免横屏视频两边出现刺眼黑边 */}
      <div
        className="shorts-slide__bg"
        style={{ backgroundImage: `url(${item.poster})` }}
        aria-hidden="true"
      />

      {shouldMount ? (
        <video
          ref={setRef}
          className="shorts-slide__video"
          src={shouldLoad ? item.videoSrc : undefined}
          poster={item.poster}
          preload={shouldLoad ? (shouldEagerLoad ? "auto" : "metadata") : "none"}
          playsInline
          loop
          muted={muted}
          controlsList="nodownload"
          disablePictureInPicture
          onContextMenu={(e) => e.preventDefault()}
        />
      ) : (
        <img
          className="shorts-slide__poster"
          src={item.poster}
          alt=""
          aria-hidden="true"
          loading="lazy"
        />
      )}

      {fastActive && (
        <div className="shorts-slide__rate-hint" aria-hidden="true">
          2x 速播放中
        </div>
      )}



      {paused && isActive && !scrubbing && (
        <div className="shorts-slide__paused" aria-hidden="true">
          ▶
        </div>
      )}

      {/* 视频加载/缓冲旋转器 */}
      {isBuffering && isActive && shouldLoad && !isMarkedHidden && (
        <div className="shorts-slide__buffering" aria-hidden="true">
          <ShortsLoadingSpinner size={30} />
        </div>
      )}

      {/* 不再展示屏蔽遮罩 */}
      {isMarkedHidden && (
        <div className="shorts-slide__hidden-overlay" onClick={(e) => e.stopPropagation()}>
          <EyeOff size={38} style={{ color: "#ff4060", marginBottom: "8px" }} />
          <div className="shorts-slide__hidden-title">已隐藏该视频</div>
          <div className="shorts-slide__hidden-desc">系统将不会再次在任何地方向您展示此视频</div>
        </div>
      )}

      <div className="shorts-slide__overlay" onClick={(e) => e.stopPropagation()}>
        <h2 className="shorts-slide__title">{item.title}</h2>
        <div className="shorts-slide__meta">
          {item.sourceLabel && (
            <span className="shorts-slide__meta-item">{item.sourceLabel}</span>
          )}
          {item.duration && (
            <span className="shorts-slide__meta-item">{item.duration}</span>
          )}
          {item.tags && item.tags.length > 0 && (
            <span className="shorts-slide__meta-item">
              {item.tags.slice(0, 3).map((t) => `#${t}`).join(" ")}
            </span>
          )}
        </div>
        <Link
          to={`/video/${encodeURIComponent(item.id)}`}
          className="shorts-slide__detail"
        >
          <Info size={13} />
          <span>查看详情</span>
        </Link>
      </div>

      {/* 右下角操作栏 */}
      <aside
        className="shorts-slide__actions"
        onClick={(e) => e.stopPropagation()}
      >
        {/* 云盘来源徽章 */}
        <div className="shorts-drive-badge" title={`来源: ${item.sourceLabel || "本地"}`}>
          {getDriveShortName(item.sourceLabel || "本地")}
        </div>

        {/* 点赞 */}
        <button
          type="button"
          className={`shorts-slide__action ${isLiked ? "is-liked" : ""}`}
          aria-label={isLiked ? "取消点赞" : "点赞"}
          aria-pressed={isLiked}
          onClick={handleHeartClick}
        >
          <Heart
            size={24}
            fill={isLiked ? "currentColor" : "none"}
            strokeWidth={2}
          />
          <span className="shorts-slide__action-count">{formatCount(likes)}</span>
        </button>



        {/* 不再展示 */}
        <button
          type="button"
          className="shorts-slide__action"
          aria-label="不再展示"
          onClick={handleHideClick}
        >
          <EyeOff size={22} />
          <span className="shorts-slide__action-count">隐藏</span>
        </button>
      </aside>

      {/* 双击点赞时弹起的心形动画 */}
      {heartBurst && (
        <div
          key={heartBurst.key}
          className="shorts-slide__heart-burst"
          style={{ left: heartBurst.x, top: heartBurst.y }}
          aria-hidden="true"
        >
          <Heart size={88} fill="currentColor" strokeWidth={0} />
        </div>
      )}

      {/* 进度条 */}
      {isActive && shouldLoad && !isMarkedHidden && (
        <div
          className={`shorts-slide__progress ${
            scrubbing ? "is-scrubbing" : ""
          }`}
          onPointerDown={handleProgressPointerDown}
          onPointerMove={handleProgressPointerMove}
          onPointerUp={handleProgressPointerEnd}
          onPointerCancel={handleProgressPointerEnd}
          onLostPointerCapture={handleProgressPointerEnd}
          onClick={(e) => e.stopPropagation()}
        >
          <div
            className="shorts-slide__progress-track"
            style={{
              "--progress-pct": `${progressRatio * 100}%`,
            } as React.CSSProperties}
          >
            <div
              className="shorts-slide__progress-fill"
              style={{ width: `${progressRatio * 100}%` }}
            />
          </div>
          {scrubbing && (
            <div className="shorts-slide__progress-time">
              {formatClock(currentTime)} / {formatClock(duration)}
            </div>
          )}
        </div>
      )}
    </article>
  );
}

function ShortsLoadingSpinner({ size }: { size: number }) {
  const ref = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    let frame = 0;
    const startedAt = performance.now();
    const tick = (now: number) => {
      const spinner = ref.current;
      if (spinner) {
        const rotation = ((now - startedAt) / 800) * 360;
        spinner.style.transform = `rotate(${rotation}deg)`;
      }
      frame = window.requestAnimationFrame(tick);
    };
    frame = window.requestAnimationFrame(tick);
    return () => window.cancelAnimationFrame(frame);
  }, []);

  return (
    <span
      ref={ref}
      className="shorts-slide__loading-spinner"
      style={{
        "--shorts-spinner-size": `${size}px`,
      } as React.CSSProperties}
      aria-hidden="true"
    />
  );
}

/**
 * 用 memo 包一层：滚动 / 切换当前屏时只让目标 slide 重渲染，
 * 其他 5 屏避免无意义的全量重渲染。
 *
 * areEqual 关键点：
 * 1. item 引用必须先比；item 来自后端 immutable，引用相同时视作相等。
 * 2. isActive / shouldMount / shouldLoad / shouldEagerLoad 决定 video 壳的 src
 *    和事件绑定，必须严格比。
 * 3. muted / volume 同步到 <video>，变了就要 re-render 当前屏。
 * 4. index 只用于本地 effect / ref 查找；列表顺序不变则相等。
 * 5. 回调（onLikeToggle / hasLiked / ...）都是父组件 useCallback 出来的，
 *    引用稳定；videoRef 通过 setVideoRefMapRef 缓存，也是稳定引用。
 *    其他回调（setMuted / setVolume）来自 useState 的 setter，React 保证稳定。
 */
const ShortsSlide = memo(ShortsSlideInner, (prev, next) => {
  if (prev.index !== next.index) return false;
  if (prev.isActive !== next.isActive) return false;
  if (prev.shouldMount !== next.shouldMount) return false;
  if (prev.shouldLoad !== next.shouldLoad) return false;
  if (prev.shouldEagerLoad !== next.shouldEagerLoad) return false;
  if (prev.muted !== next.muted) return false;
  if (prev.volume !== next.volume) return false;
  if (prev.item !== next.item) return false;
  return true;
});

function applyVideoAudioState(
  video: HTMLVideoElement,
  nextMuted: boolean,
  nextVolume: number
) {
  const safeVolume = clamp(nextVolume, 0, 1);
  const syncVolume = () => {
    try {
      if (Math.abs(video.volume - safeVolume) > 0.001) {
        video.volume = safeVolume;
      }
    } catch {
      // Some mobile browsers expose volume as effectively read-only.
    }
  };

  if (!nextMuted) syncVolume();
  try {
    if (video.muted !== nextMuted) {
      video.muted = nextMuted;
    }
  } catch {
    // ignore
  }
  if (nextMuted) syncVolume();
}

function normalizeVideoPlaybackRate(video: HTMLVideoElement) {
  try {
    if (video.defaultPlaybackRate !== 1) {
      video.defaultPlaybackRate = 1;
    }
    if (video.playbackRate !== 1) {
      video.playbackRate = 1;
    }
  } catch {
    // ignore
  }
}

function stabilizeVideoAfterAudioToggle(
  video: HTMLVideoElement,
  shouldResume: () => boolean
) {
  const stabilize = () => {
    normalizeVideoPlaybackRate(video);
    if (shouldResume() && video.paused && !video.ended) {
      video.play().catch(() => undefined);
    }
  };

  stabilize();
  for (const delay of [80, 240, 600]) {
    window.setTimeout(stabilize, delay);
  }
}

function shouldUseDocumentScrollForShorts() {
  return isIPhoneBrowserShell();
}

function isIPhoneBrowserShell() {
  if (typeof window === "undefined" || typeof navigator === "undefined") {
    return false;
  }
  const ua = navigator.userAgent || "";
  return /\biPhone\b|\biPod\b/.test(ua) && !isStandaloneDisplayMode();
}

function isStandaloneDisplayMode() {
  if (typeof window === "undefined" || typeof navigator === "undefined") {
    return false;
  }
  const nav = navigator as Navigator & { standalone?: boolean };
  return (
    nav.standalone === true ||
    window.matchMedia?.("(display-mode: standalone)").matches === true ||
    window.matchMedia?.("(display-mode: fullscreen)").matches === true
  );
}

function supportsElementFullscreenAPI(target?: Element | null) {
  if (typeof document === "undefined") return false;
  const el = (target ?? document.documentElement) as HTMLElement & {
    webkitRequestFullscreen?: () => Promise<void> | void;
  };
  return (
    typeof el.requestFullscreen === "function" ||
    typeof el.webkitRequestFullscreen === "function"
  );
}

function clamp(n: number, min: number, max: number) {
  return n < min ? min : n > max ? max : n;
}

function getVideoWindowBounds(highestViewedIndex: number, itemCount: number) {
  const size = Math.min(VIDEO_WINDOW_SIZE, itemCount);
  if (size <= 0 || highestViewedIndex < 0) return { start: 0, end: -1 };

  const end = clamp(highestViewedIndex, 0, itemCount - 1);
  const start = Math.max(0, end - size + 1);
  return { start, end };
}

/** 已经缓冲到片尾（含误差余量），不会再因网络卡顿 */
function videoBufferedToEnd(video: HTMLVideoElement) {
  const duration = Number.isFinite(video.duration) ? video.duration : 0;
  if (duration <= 0) return false;
  const remaining = Math.max(0, duration - (video.currentTime || 0));
  return bufferedAheadSeconds(video) >= remaining - 0.25;
}

function videoHasBufferedData(video: HTMLVideoElement) {
  for (let i = 0; i < video.buffered.length; i += 1) {
    if (video.buffered.end(i) > video.buffered.start(i)) {
      return true;
    }
  }
  return false;
}

/** 前向缓冲健康（达到高水位或已缓冲到结尾），可以放心预加载后续视频 */
function videoHasComfortableBuffer(video: HTMLVideoElement) {
  if (video.readyState < 3) return false;
  if (videoBufferedToEnd(video)) return true;
  return bufferedAheadSeconds(video) >= ACTIVE_PRELOAD_BUFFER_SECONDS;
}

/** 前向缓冲告急（跌破低水位且没缓冲到结尾），应收回预加载授权 */
function videoBufferIsCritical(video: HTMLVideoElement) {
  if (video.readyState < 3) return true;
  if (videoBufferedToEnd(video)) return false;
  return bufferedAheadSeconds(video) < ACTIVE_PRELOAD_KEEP_SECONDS;
}

function bufferedAheadSeconds(video: HTMLVideoElement) {
  const current = video.currentTime || 0;
  for (let i = 0; i < video.buffered.length; i += 1) {
    const start = video.buffered.start(i);
    const end = video.buffered.end(i);
    if (start <= current + 0.25 && end > current) {
      return Math.max(0, end - current);
    }
  }
  return 0;
}

function formatClock(seconds: number) {
  if (!Number.isFinite(seconds) || seconds < 0) return "00:00";
  const total = Math.floor(seconds);
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

/** 简易的点赞数缩写：1.2k / 3.4w，避免 5 位数挤爆右侧操作栏 */
function formatCount(n: number) {
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n < 1000) return String(n);
  if (n < 10000) return (n / 1000).toFixed(1).replace(/\.0$/, "") + "k";
  return (n / 10000).toFixed(1).replace(/\.0$/, "") + "w";
}

/** 识别云盘缩写名称 */
function getDriveShortName(source: string): string {
  const s = source.toLowerCase();
  if (s.includes("115")) return "115";
  if (s.includes("123")) return "123";
  if (s.includes("pikpak")) return "PikP";
  if (s.includes("quark") || s.includes("夸克")) return "Quak";
  if (s.includes("onedrive")) return "OneDrive";
  if (s.includes("wopan") || s.includes("沃盘")) return "沃盘";
  if (s.includes("guangyapan") || s.includes("guangya") || s.includes("光鸭")) return "光鸭";
  if (s.includes("localstorage") || s.includes("本地")) return "本地";
  if (s.includes("spider") || s.includes("爬虫")) return "爬虫";
  return source.substring(0, 4);
}
