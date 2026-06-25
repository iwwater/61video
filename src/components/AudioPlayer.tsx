import { useEffect, useRef, useState } from "react";
import { Clock, Music4, SkipBack, SkipForward } from "lucide-react";

type LoopMode = "off" | "all" | "one";

type Props = {
  /** 当前播放的音频直链 (mediaSrc/videoSrc) */
  src: string;
  /** 海报图(可空:空时显示 Music4 占位) */
  poster?: string;
  /** 当前曲目标题 */
  title: string;
  /** 上一首按钮是否可用 */
  hasPrev: boolean;
  /** 下一首按钮是否可用(也是队列末尾判定) */
  hasNext: boolean;
  /** 点击上一首(队列头时禁用,这里再加一道防御) */
  onPrev: () => void;
  /** 点击下一首 / 当前曲自然结束时也会触发,用于自动连播 */
  onNext: () => void;
  /** 首次播放上报(进 continue watching) */
  onPlay?: () => void;
  /** 播放进度上报(每 ~3s 一次,父组件自行节流) */
  onProgress?: (seconds: number) => void;
  /** 循环模式:off=停,all=全部循环,one=单曲循环(audio.loop=true) */
  loopMode: LoopMode;
  onLoopModeChange: (mode: LoopMode) => void;
  /**
   * compact=true:不渲染大 hero 区块(标题 + 海报 + Music4 圆形占位),
   * 只渲染内联控件。详情页在大唱片布局里用这个模式,大封面/标题由父组件渲染。
   */
  compact?: boolean;
};

// 倍速档位
const SPEEDS = [0.75, 1, 1.5, 2] as const;
const SPEED_STORAGE_KEY = "audio-player.speed";

// 睡眠定时器档位 (分钟)。0 = 关。
const TIMER_OPTIONS_MIN = [0, 15, 30, 60] as const;
type TimerMinutes = (typeof TIMER_OPTIONS_MIN)[number];

function readStoredSpeed(): number {
  if (typeof window === "undefined") return 1;
  const raw = window.localStorage.getItem(SPEED_STORAGE_KEY);
  if (!raw) return 1;
  const n = Number(raw);
  return (SPEEDS as readonly number[]).includes(n) ? n : 1;
}

function formatMmSs(sec: number): string {
  const s = Math.max(0, Math.floor(sec));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/**
 * 音频播放器。
 *
 * 设计点:
 * - 队列状态由父组件持有(VideoDetailPage)。本组件只负责"播当前 src + 上下首 + 自动连播"。
 * - 切换曲目时父组件 navigate 到新 id,本页 detail 重抓,src 变化触发自动播放。
 *   这里用 isFirstSrc ref 区分"挂载时的 src"和"切歌时的 src",避免进入页面就自动响。
 * - 进度上报节流由父组件做(同 video 共用 handleProgress,3s 阈值),本组件只透传 currentTime。
 * - 倍速:localStorage 持久化,跨刷新保留。
 * - 睡眠定时器:不持久化(离开页就停)。状态 = (timerMinutes, timerStartAt),
 *   UI 每秒 tick 一次反算剩余秒数显示;到点 pause + 设回 0。
 */
export function AudioPlayer({
  src,
  poster,
  title,
  hasPrev,
  hasNext,
  onPrev,
  onNext,
  onPlay,
  onProgress,
  loopMode,
  onLoopModeChange,
  compact = false,
}: Props) {
  const audioRef = useRef<HTMLAudioElement>(null);
  const isFirstSrc = useRef(true);

  // 倍速
  const [speed, setSpeed] = useState<number>(() => readStoredSpeed());
  useEffect(() => {
    if (audioRef.current) audioRef.current.playbackRate = speed;
    if (typeof window !== "undefined") {
      window.localStorage.setItem(SPEED_STORAGE_KEY, String(speed));
    }
  }, [speed]);

  // 睡眠定时器
  const [timerMinutes, setTimerMinutes] = useState<TimerMinutes>(0);
  const [timerStartAt, setTimerStartAt] = useState<number>(0);
  // 用 setTick 触发每秒一次的 re-render,只为更新倒计时显示。
  const [, setTick] = useState(0);
  useEffect(() => {
    if (timerMinutes === 0) return;
    const endsAt = timerStartAt + timerMinutes * 60_000;
    const id = window.setInterval(() => {
      if (Date.now() >= endsAt) {
        audioRef.current?.pause();
        setTimerMinutes(0);
        window.clearInterval(id);
      } else {
        setTick((t) => (t + 1) % 1_000_000);
      }
    }, 1000);
    return () => window.clearInterval(id);
  }, [timerMinutes, timerStartAt]);

  // src 变化时(切歌)自动播放;首次挂载不播
  useEffect(() => {
    if (isFirstSrc.current) {
      isFirstSrc.current = false;
      return;
    }
    audioRef.current?.play().catch(() => undefined);
  }, [src]);

  // 循环模式:"one" 时设 audio.loop=true,让浏览器自动重播;off/all 时由 onEnded 走 JS 逻辑
  useEffect(() => {
    if (audioRef.current) {
      audioRef.current.loop = loopMode === "one";
    }
  }, [loopMode]);

  function handlePrevClick() {
    if (!hasPrev) return;
    onPrev();
  }
  function handleNextClick() {
    if (!hasNext) return;
    onNext();
  }
  function handleEnded() {
    // 单曲循环:audio.loop=true 已自动重播,这里不主动切
    if (loopMode === "one") return;
    if (hasNext) onNext();
  }
  function handleTimeUpdate(e: React.SyntheticEvent<HTMLAudioElement>) {
    const seconds = e.currentTarget.currentTime;
    onProgress?.(seconds);
  }
  function pickTimer(min: TimerMinutes) {
    if (min === 0) {
      setTimerMinutes(0);
      return;
    }
    setTimerMinutes(min);
    setTimerStartAt(Date.now());
  }

  // 倒计时显示文本 (mm:ss)
  const remainingSec =
    timerMinutes > 0
      ? Math.max(0, Math.ceil((timerStartAt + timerMinutes * 60_000 - Date.now()) / 1000))
      : 0;
  const timerDisplay = timerMinutes > 0 ? formatMmSs(remainingSec) : null;

  return (
    <div className={`audio-player${compact ? " audio-player--compact" : ""}`}>
      {!compact && (
        <div
          className="audio-player__hero"
          style={poster ? { backgroundImage: `url(${poster})` } : undefined}
        >
          <div className="audio-player__overlay" />
          <div className="audio-player__content">
            <span className="audio-player__icon" aria-hidden="true">
              <Music4 size={28} />
            </span>
            <div className="audio-player__meta">
              <span className="audio-player__eyebrow">Audio</span>
              <strong className="audio-player__title">{title}</strong>
            </div>
          </div>
        </div>
      )}
      <div className="audio-player__controls">
        <audio
          ref={audioRef}
          controls
          preload="metadata"
          src={src}
          className="audio-player__native"
          onPlay={onPlay}
          onEnded={handleEnded}
          onTimeUpdate={handleTimeUpdate}
        />
        <div className="audio-player__nav" role="group" aria-label="队列控制">
          <button
            type="button"
            className="audio-player__nav-btn"
            onClick={handlePrevClick}
            disabled={!hasPrev}
            aria-label="上一首"
            title="上一首"
          >
            <SkipBack size={18} />
          </button>
          <button
            type="button"
            className="audio-player__nav-btn"
            onClick={handleNextClick}
            disabled={!hasNext}
            aria-label="下一首"
            title="下一首"
          >
            <SkipForward size={18} />
          </button>
        </div>
        <div className="audio-player__extras" role="group" aria-label="播放选项">
          <div className="audio-player__speed" role="group" aria-label="倍速">
            <span className="audio-player__extras-label">倍速</span>
            {SPEEDS.map((s) => (
              <button
                key={s}
                type="button"
                className={`audio-player__chip${speed === s ? " is-active" : ""}`}
                onClick={() => setSpeed(s)}
                aria-pressed={speed === s}
                title={`播放速度 ${s}x`}
              >
                {s}x
              </button>
            ))}
          </div>
          <div className="audio-player__timer" role="group" aria-label="睡眠定时">
            <Clock size={14} aria-hidden="true" />
            <span className="audio-player__extras-label">定时</span>
            {TIMER_OPTIONS_MIN.map((min) => {
              const active = min === timerMinutes;
              return (
                <button
                  key={min}
                  type="button"
                  className={`audio-player__chip${active ? " is-active" : ""}`}
                  onClick={() => pickTimer(min)}
                  aria-pressed={active}
                  title={min === 0 ? "关闭定时" : `${min} 分钟后停止`}
                >
                  {min === 0 ? "关" : `${min}分`}
                </button>
              );
            })}
            {timerDisplay && (
              <span className="audio-player__timer-count" aria-live="polite">
                {timerDisplay}
              </span>
            )}
          </div>
          <div className="audio-player__loop" role="group" aria-label="循环模式">
            <span className="audio-player__extras-label">循环</span>
            {([
              ["off", "关"],
              ["all", "全部"],
              ["one", "单曲"],
            ] as const).map(([value, label]) => {
              const active = loopMode === value;
              return (
                <button
                  key={value}
                  type="button"
                  className={`audio-player__chip${active ? " is-active" : ""}`}
                  onClick={() => onLoopModeChange(value)}
                  aria-pressed={active}
                  title={
                    value === "off"
                      ? "不循环"
                      : value === "all"
                        ? "队列到末尾自动回到第一首"
                        : "单曲循环"
                  }
                >
                  {label}
                </button>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
