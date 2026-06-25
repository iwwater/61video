import { Link } from "react-router-dom";
import { History, Play } from "lucide-react";
import type { VideoItem } from "@/types";
import { formatCount } from "@/lib/format";

type MediaFilter = "video" | "audio";

type Props = {
  videos: VideoItem[];
  /**
   * 媒体类型过滤:不传则显示全部(标题 "继续观看");
   * 传 "video" 显示视频(标题 "继续观看");"audio" 显示音频(标题 "继续收听")。
   * 列表内容由父组件预先筛好;这里只在客户端按 mediaType 再过滤一次保险。
   */
  mediaType?: MediaFilter;
};

function pickRailTitle(filter: MediaFilter | undefined): { title: string; aria: string; unit: string } {
  if (filter === "audio") return { title: "继续收听", aria: "继续收听", unit: "首" };
  return { title: "继续观看", aria: "继续观看", unit: "部" };
}

/**
 * 首页"继续观看/收听" rail。看了一半的媒体（5%~95%），按 progress_at 倒序。
 * 卡片底部带 1px 进度条，hover 缩略图右上角有 ▶ 图标提示"接着看/听"。
 */
export function ContinueWatchingRail({ videos, mediaType }: Props) {
  const filtered = mediaType
    ? videos.filter((v) => (v.mediaType ?? "video") === mediaType)
    : videos;
  if (!filtered || filtered.length === 0) return null;
  const t = pickRailTitle(mediaType);
  return (
    <section className="cw-rail" aria-label={t.aria}>
      <header className="cw-rail__head">
        <History size={18} aria-hidden="true" />
        <h2 className="cw-rail__title">{t.title}</h2>
        <span className="cw-rail__count">{filtered.length} {t.unit}</span>
      </header>
      <ul className="cw-rail__list">
        {filtered.map((v) => (
          <ContinueWatchingItem key={v.id} video={v} />
        ))}
      </ul>
    </section>
  );
}

function ContinueWatchingItem({ video }: { video: VideoItem }) {
  const dur = video.durationSeconds ?? 0;
  const prog = video.progressSeconds ?? 0;
  const pct = dur > 0 ? Math.min(100, Math.max(0, (prog / dur) * 100)) : 0;
  return (
    <li className="cw-rail__item">
      <Link to={video.href} className="cw-rail__link">
        <div className="cw-rail__thumb">
          {video.thumbnail ? (
            <img src={video.thumbnail} alt={video.title} width={180} height={101} loading="lazy" />
          ) : (
            <div className="cw-rail__thumb-placeholder" aria-hidden="true" />
          )}
          <span className="cw-rail__resume" aria-hidden="true">
            <Play size={14} fill="currentColor" />
            <span>接着看</span>
          </span>
          {video.duration ? (
            <span className="cw-rail__duration">{video.duration}</span>
          ) : null}
          <div
            className="cw-rail__progress"
            role="progressbar"
            aria-valuenow={Math.round(pct)}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={`已观看 ${Math.round(pct)}%`}
          >
            <div className="cw-rail__progress-bar" style={{ width: `${pct}%` }} />
          </div>
        </div>
        <div className="cw-rail__body">
          <h3 className="cw-rail__title-text" title={video.title}>
            {video.title}
          </h3>
          <div className="cw-rail__meta">
            {video.author ? (
              <span className="cw-rail__author">{video.author}</span>
            ) : null}
            <span>{formatCount(video.views)} 观看</span>
          </div>
        </div>
      </Link>
    </li>
  );
}
