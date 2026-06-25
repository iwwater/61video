import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ListMusic } from "lucide-react";
import { AppShell } from "@/components/AppShell";
import { VideoPlayer } from "@/components/VideoPlayer";
import { AudioPlayer } from "@/components/AudioPlayer";
import { VideoActions } from "@/components/VideoActions";
import { VideoMetaHeader } from "@/components/VideoMetaHeader";
import { VideoInfoPanel } from "@/components/VideoInfoPanel";
import { RecommendedRail } from "@/components/RecommendedRail";
import {
  deleteVideo,
  fetchListing,
  fetchTags,
  fetchVideoDetail,
  recordView,
  updateVideoTags,
} from "@/data/videos";
import type { TagItem, VideoDetail, VideoItem } from "@/types";

// 队列持久化 + 循环模式:跨刷新保留
const QUEUE_STORAGE_KEY = "audio-detail.queue.v1";
const LOOP_STORAGE_KEY = "audio-player.loop";

type SavedQueue = { currentId: string; items: VideoItem[] };

function loadSavedQueue(): SavedQueue | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(QUEUE_STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (
      !parsed ||
      typeof parsed.currentId !== "string" ||
      !Array.isArray(parsed.items)
    )
      return null;
    return parsed as SavedQueue;
  } catch {
    return null;
  }
}

function saveQueue(currentId: string, items: VideoItem[]) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(
      QUEUE_STORAGE_KEY,
      JSON.stringify({ currentId, items })
    );
  } catch {
    // 容量满 / 隐私模式:放弃持久化,不影响内存队列使用。
  }
}

type LoopMode = "off" | "all" | "one";

function loadStoredLoop(): LoopMode {
  if (typeof window === "undefined") return "off";
  const raw = window.localStorage.getItem(LOOP_STORAGE_KEY);
  if (raw === "off" || raw === "all" || raw === "one") return raw;
  return "off";
}

export default function VideoDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [detail, setDetail] = useState<VideoDetail | null>(null);
  const [tags, setTags] = useState<TagItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [tagSaving, setTagSaving] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteSource, setDeleteSource] = useState(false);
  const [deleteSaving, setDeleteSaving] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  // 音频队列:detail 是音频时拉一份 audio listing,作为上一首/下一首的导航数据
  // 初次从 localStorage 还原(如果有),让刷新后队列还在
  const [audioQueue, setAudioQueue] = useState<VideoItem[]>(
    () => loadSavedQueue()?.items ?? []
  );
  // 循环模式
  const [loopMode, setLoopMode] = useState<LoopMode>(loadStoredLoop);
  useEffect(() => {
    if (typeof window !== "undefined") {
      window.localStorage.setItem(LOOP_STORAGE_KEY, loopMode);
    }
  }, [loopMode]);
  const detailTopRef = useRef<HTMLDivElement | null>(null);

  const isAudio = detail?.mediaType === "audio";
  const audioIndex = useMemo(
    () => (isAudio ? audioQueue.findIndex((v) => v.id === detail?.id) : -1),
    [isAudio, audioQueue, detail?.id]
  );
  const hasPrevAudio = audioIndex > 0;
  const hasNextAudio = audioIndex >= 0 && audioIndex < audioQueue.length - 1;

  function jumpToAudioItem(targetId: string) {
    navigate(`/video/${encodeURIComponent(targetId)}`);
  }
  function handlePrevAudio() {
    if (!hasPrevAudio) return;
    jumpToAudioItem(audioQueue[audioIndex - 1].id);
  }
  function handleNextAudio() {
    if (hasNextAudio) {
      jumpToAudioItem(audioQueue[audioIndex + 1].id);
      return;
    }
    // 队列末尾:loopMode="all" 回到第一首;off/one 都停
    if (loopMode === "all" && audioQueue.length > 0) {
      jumpToAudioItem(audioQueue[0].id);
    }
  }

  useEffect(() => {
    if (!id) return;
    let active = true;
    window.scrollTo({ top: 0, behavior: "auto" });
    setLoading(true);
    Promise.all([fetchVideoDetail(id), fetchTags()]).then(([d, tagList]) => {
      if (!active) return;
      setDetail(d);
      setTags(tagList);
      setLoading(false);
      document.title = d ? `${d.title} · 61` : "视频不存在";
    });
    return () => {
      active = false;
    };
  }, [id]);

  // 音频:拉一份 audio listing 作为上下首队列。第一页 100 条对个人 ASMR 已够用,
  // 不够再说(不预先实现"按需翻页"避免无谓复杂度)。
  useEffect(() => {
    if (!isAudio) {
      setAudioQueue([]);
      return;
    }
    let active = true;
    fetchListing(1, 100, { mediaType: "audio" }).then((r) => {
      if (!active) return;
      setAudioQueue(r.items ?? []);
    });
    return () => {
      active = false;
    };
  }, [isAudio]);

  // 队列持久化:每次队列或当前曲目变化时,写 localStorage。
  // 失败(quota / 隐私模式)静默忽略,不影响内存使用。
  useEffect(() => {
    if (!isAudio || audioQueue.length === 0 || !detail?.id) return;
    saveQueue(detail.id, audioQueue);
  }, [isAudio, audioQueue, detail?.id]);

  useLayoutEffect(() => {
    if (loading || !detail) return;
    window.requestAnimationFrame(() => {
      detailTopRef.current?.scrollIntoView({
        block: "start",
        behavior: "auto",
      });
    });
  }, [loading, detail?.id]);

  async function handleTagsChange(nextTags: string[]) {
    if (!detail) return;
    setTagSaving(true);
    try {
      const updated = await updateVideoTags(detail.id, nextTags);
      setDetail({ ...detail, tags: updated.tags ?? [] });
    } finally {
      setTagSaving(false);
    }
  }

  function handleOpenDelete() {
    if (!detail || deleteSaving) return;
    setDeleteSource(false);
    setDeleteError("");
    setDeleteOpen(true);
  }

  function handleCloseDelete() {
    if (deleteSaving) return;
    setDeleteOpen(false);
    setDeleteError("");
  }

  async function handleConfirmDelete() {
    if (!detail || deleteSaving) return;
    setDeleteSaving(true);
    setDeleteError("");
    try {
      await deleteVideo(detail.id, { deleteSource });
      navigate("/list", { replace: true });
    } catch {
      setDeleteError(
        deleteSource
          ? "删除失败。源文件未能删除时，管理库记录会保留。"
          : "删除失败，请稍后重试。"
      );
      setDeleteSaving(false);
    }
  }

  function handleFirstPlay() {
    if (!detail) return;
    // 失败静默忽略，不打扰用户播放体验
    recordView(detail.id).catch(() => undefined);
  }

  // 续播：每 ~5s 上报一次 currentTime。失败静默。
  const lastReportedRef = useRef(0);
  function handleProgress(seconds: number) {
    if (!detail) return;
    // 简单节流：值变化 >= 3s 才报一次
    if (Math.abs(seconds - lastReportedRef.current) < 3) return;
    lastReportedRef.current = seconds;
    fetch(`/api/video/${encodeURIComponent(detail.id)}/progress`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ seconds }),
    }).catch(() => undefined);
  }

  if (loading) {
    return (
      <AppShell mobileAutoHideNav>
        <div className="vd-page">
          <div className="vd-ambient" aria-hidden="true" />
          <div className="container vd-page__inner">
            <div
              className="vd-layout vd-skeleton"
              aria-busy="true"
              aria-label="视频详情加载中"
            >
              <div className="vd-main">
                <div className="vd-skeleton__player" />

                <div className="vd-skeleton__summary">
                  <div className="vd-skeleton__chips">
                    <span className="vd-skeleton__chip vd-skeleton__chip--source" />
                    <span className="vd-skeleton__chip" />
                    <span className="vd-skeleton__chip vd-skeleton__chip--plain" />
                    <span className="vd-skeleton__chip vd-skeleton__chip--plain" />
                  </div>
                  <div className="vd-skeleton__title" />
                  <div className="vd-skeleton__actions">
                    <span />
                    <span />
                    <span />
                  </div>
                </div>

                <div className="vd-skeleton__info">
                  <span className="vd-skeleton__section-head" />
                  <span className="vd-skeleton__line" />
                  <span className="vd-skeleton__line vd-skeleton__line--short" />
                  <div className="vd-skeleton__tag-row">
                    <span />
                    <span />
                    <span />
                  </div>
                </div>
              </div>

              <aside className="vd-rail vd-skeleton__rail">
                <div className="vd-rail__head">
                  <span className="vd-rail__head-icon" aria-hidden="true">
                    <span />
                    <span />
                  </span>
                  <span className="vd-skeleton__rail-head" />
                </div>
                <ul className="vd-rail__list vd-skeleton__rail-list">
                  {Array.from({ length: 6 }).map((_, index) => (
                    <li key={index} className="vd-skeleton__rail-item">
                      <span className="vd-skeleton__rail-thumb" />
                      <span className="vd-skeleton__rail-body">
                        <span className="vd-skeleton__rail-title" />
                        <span className="vd-skeleton__rail-title vd-skeleton__rail-title--short" />
                        <span className="vd-skeleton__rail-meta" />
                      </span>
                    </li>
                  ))}
                </ul>
              </aside>
            </div>
          </div>
        </div>
      </AppShell>
    );
  }

  if (!detail) {
    return (
      <AppShell mobileAutoHideNav>
        <div className="vd-page">
          <div className="container vd-page__inner">
            <div className="vd-empty">视频不存在或已被移除</div>
          </div>
        </div>
      </AppShell>
    );
  }

  return (
    <AppShell mobileAutoHideNav>
      <div className="vd-page">
        {/* Ambient 背景层：用海报作模糊底色，叠加渐变过渡到页面背景 */}
        <div
          className="vd-ambient"
          aria-hidden="true"
          style={{
            backgroundImage: detail.poster
              ? `url(${detail.poster})`
              : undefined,
          }}
        />

        <div className="container vd-page__inner">
          <div className="vd-layout">
            <div className="vd-main" ref={detailTopRef}>
              <div className="vd-player-wrap">
                <div className="vd-player">
                  {detail.mediaType === "audio" ? (
                    <AudioPlayer
                      src={detail.mediaSrc || detail.videoSrc}
                      poster={detail.poster}
                      title={detail.title}
                      hasPrev={hasPrevAudio}
                      hasNext={hasNextAudio}
                      onPrev={handlePrevAudio}
                      onNext={handleNextAudio}
                      onPlay={handleFirstPlay}
                      onProgress={handleProgress}
                      loopMode={loopMode}
                      onLoopModeChange={setLoopMode}
                    />
                  ) : (
                    <VideoPlayer
                      id={detail.id}
                      src={detail.mediaSrc || detail.videoSrc}
                      poster={detail.poster}
                      previewSrc={detail.previewSrc}
                      title={detail.title}
                      initialTime={detail.progressSeconds}
                      onFirstPlay={handleFirstPlay}
                      onProgress={handleProgress}
                    />
                  )}
                </div>
              </div>

              <section className="vd-summary" aria-label="当前视频">
                <VideoMetaHeader video={detail} />

                <VideoActions
                  video={detail}
                  onDeleteVideo={handleOpenDelete}
                  deleteSaving={deleteSaving}
                />
              </section>

              <VideoInfoPanel
                video={detail}
                availableTags={tags}
                tagSaving={tagSaving}
                onTagsChange={handleTagsChange}
              />

              {isAudio && audioIndex >= 0 && audioQueue.length > 1 && (
                <AudioUpNext
                  queue={audioQueue}
                  currentIndex={audioIndex}
                  onJump={jumpToAudioItem}
                />
              )}
            </div>

            {/* 右侧推荐视频不与音频场景契合（音频用户更关心队列/同专辑/同作者），
                视频详情保留。 */}
            {!isAudio && <RecommendedRail videos={detail.relatedVideos} />}
          </div>
        </div>
      </div>

      {deleteOpen && (
        <div className="vd-delete-modal" role="presentation">
          <div
            className="vd-delete-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="vd-delete-title"
          >
            <div className="vd-delete-head">
              <h2 id="vd-delete-title" className="vd-delete-title">
                删除视频
              </h2>
              <p className="vd-delete-text">
                确定删除「{detail.title}」吗？此操作会从管理库移除该视频。
              </p>
            </div>

            <label className="vd-delete-option">
              <input
                type="checkbox"
                checked={deleteSource}
                disabled={deleteSaving}
                onChange={(e) => setDeleteSource(e.target.checked)}
              />
              <span>
                <strong>同时删除网盘中的源文件</strong>
              </span>
            </label>

            {deleteError && <div className="vd-delete-error">{deleteError}</div>}

            <div className="vd-delete-actions">
              <button
                type="button"
                className="vd-delete-action vd-delete-cancel"
                onClick={handleCloseDelete}
                disabled={deleteSaving}
              >
                取消
              </button>
              <button
                type="button"
                className="vd-delete-action vd-delete-confirm"
                onClick={handleConfirmDelete}
                disabled={deleteSaving}
              >
                {deleteSaving ? "删除中..." : "删除"}
              </button>
            </div>
          </div>
        </div>
      )}
    </AppShell>
  );
}

/**
 * 音频详情页下方的"队列"列表。
 * 显示当前曲目 ± 几条(2 前 + 7 后),让用户能直接跳到想听的那首。
 * 点击任一条 → 父组件 navigate 到 /video/<id> → 详情页重抓 → AudioPlayer 自动连播。
 */
function AudioUpNext({
  queue,
  currentIndex,
  onJump,
}: {
  queue: VideoItem[];
  currentIndex: number;
  onJump: (id: string) => void;
}) {
  const windowSize = 10;
  const halfBefore = 2;
  const start = Math.max(0, currentIndex - halfBefore);
  const end = Math.min(queue.length, start + windowSize);
  // 如果 start 已经被 clamp 到 0,把窗口尽量往后延,保证窗口填满
  const adjustedStart = Math.max(0, end - windowSize);
  const visible = queue.slice(adjustedStart, end);

  return (
    <section className="vd-audio-upnext" aria-label="队列">
      <header className="vd-audio-upnext__head">
        <ListMusic size={16} aria-hidden="true" />
        <span className="vd-audio-upnext__title">队列</span>
        <span className="vd-audio-upnext__pos">
          {currentIndex + 1} / {queue.length}
        </span>
      </header>
      <ul className="vd-audio-upnext__list">
        {visible.map((v) => {
          const isCurrent = v.id === queue[currentIndex]?.id;
          return (
            <li
              key={v.id}
              className={`vd-audio-upnext__item${isCurrent ? " is-current" : ""}`}
            >
              <button
                type="button"
                className="vd-audio-upnext__btn"
                onClick={() => onJump(v.id)}
                aria-current={isCurrent ? "true" : undefined}
              >
                <span className="vd-audio-upnext__name">{v.title}</span>
                {v.duration ? (
                  <span className="vd-audio-upnext__dur">{v.duration}</span>
                ) : null}
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
