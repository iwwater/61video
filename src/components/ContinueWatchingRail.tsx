import { Link } from "react-router-dom";
import { History, Play } from "lucide-react";
import type { VideoItem } from "@/types";
import { formatCount } from "@/lib/format";

type Props = {
  videos: VideoItem[];
};

/**
 * 首页顶部"继续观看" rail。看了一半的视频（5%~95%），按 progress_at 倒序。
 * 卡片底部带 1px 进度条，hover 缩略图右上角有 ▶ 图标提示"接着看"。
 */
export function ContinueWatchingRail({ videos }: Props) {
  if (!videos || videos.length === 0) return null;
  return (
    <section className="cw-rail" aria-label="继续观看">
      <header className="cw-rail__head">
        <History size={18} aria-hidden="true" />
        <h2 className="cw-rail__title">继续观看</h2>
        <span className="cw-rail__count">{videos.length} 部</span>
      </header>
      <ul className="cw-rail__list">
        {videos.map((v) => (
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
            <img src={video.thumbnail} alt={video.title} loading="lazy" />
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
