import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, Sparkles } from "lucide-react";
import { VideoCard } from "./VideoCard";
import type { VideoItem } from "@/types";

/**
 * 首页顶部"P1 精选"横滚卡片条。
 *
 * 设计目标：
 *   - 横滚 5+ 个 VideoCard（实际 mock 10 个，后续接 /api/home/promo）
 *   - 左右箭头按钮：每次滚动 ~80% 容器宽度
 *   - 移动端隐藏箭头，依赖原生 touch scroll
 *   - 复用 VideoCard，行为完全一致（hover 预览、点击进详情等）
 */
export function PromoStrip({ videos }: { videos?: VideoItem[] } = {}) {
  const items = videos && videos.length > 0 ? videos : MOCK_PROMO_VIDEOS;
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const [canPrev, setCanPrev] = useState(false);
  const [canNext, setCanNext] = useState(false);

  const updateButtons = useCallback(() => {
    const el = scrollerRef.current;
    if (!el) return;
    const { scrollLeft, scrollWidth, clientWidth } = el;
    // 留 2px 缓冲避免 subpixel 抖动
    setCanPrev(scrollLeft > 2);
    setCanNext(scrollLeft + clientWidth < scrollWidth - 2);
  }, []);

  useEffect(() => {
    updateButtons();
    const el = scrollerRef.current;
    if (!el) return;
    el.addEventListener("scroll", updateButtons, { passive: true });
    window.addEventListener("resize", updateButtons);
    return () => {
      el.removeEventListener("scroll", updateButtons);
      window.removeEventListener("resize", updateButtons);
    };
  }, [updateButtons, items.length]);

  function scrollByPage(dir: 1 | -1) {
    const el = scrollerRef.current;
    if (!el) return;
    const delta = el.clientWidth * 0.8 * dir;
    el.scrollBy({ left: delta, behavior: "smooth" });
  }

  if (items.length === 0) return null;

  return (
    <section className="promo-strip" aria-label="P1 精选">
      <header className="promo-strip__head">
        <span className="promo-strip__head-icon" aria-hidden="true">
          <Sparkles size={18} />
        </span>
        <h2 className="promo-strip__head-title">P1 精选</h2>
        <span className="promo-strip__count">{items.length} 部</span>
        <div className="promo-strip__nav">
          <button
            type="button"
            className="promo-strip__nav-btn"
            onClick={() => scrollByPage(-1)}
            disabled={!canPrev}
            aria-label="向前滚动"
          >
            <ChevronLeft size={18} />
          </button>
          <button
            type="button"
            className="promo-strip__nav-btn"
            onClick={() => scrollByPage(1)}
            disabled={!canNext}
            aria-label="向后滚动"
          >
            <ChevronRight size={18} />
          </button>
        </div>
      </header>
      <div className="promo-strip__scroller" ref={scrollerRef}>
        {items.map((v, i) => (
          <div className="promo-strip__item" key={v.id}>
            <VideoCard video={v} priority={i < 4} />
          </div>
        ))}
      </div>
    </section>
  );
}

/**
 * Mock 数据。后续接到 /api/home/promo 时把 props.videos 传进来即可，
 * 这里默认渲染兜底数据，让首屏不至于空荡荡。
 *
 * 关键字段必须满足 VideoItem：
 *   id / mediaType / href / title / thumbnail / previewSrc /
 *   previewDuration / previewStrategy / duration / badges / author /
 *   views / publishedAt
 */
const MOCK_PROMO_VIDEOS: VideoItem[] = [
  {
    id: "p1-promo-1",
    mediaType: "video",
    href: "/video/p1-promo-1",
    title: "P1 精选 · 城市夜景延时摄影合集",
    thumbnail: "/p/thumb/p1-promo-1.jpg",
    previewSrc: "/p/preview/p1-promo-1.mp4",
    previewDuration: 5,
    previewStrategy: "teaser-file",
    duration: "12:34",
    badges: ["精选", "4K"],
    quality: "HD",
    author: "P1 Studio",
    views: 184320,
    publishedAt: "3 天前",
    sourceLabel: "本地",
  },
  {
    id: "p1-promo-2",
    mediaType: "video",
    href: "/video/p1-promo-2",
    title: "P1 精选 · 山野徒步 VR 体验",
    thumbnail: "/p/thumb/p1-promo-2.jpg",
    previewSrc: "/p/preview/p1-promo-2.mp4",
    previewDuration: 5,
    previewStrategy: "teaser-file",
    duration: "08:21",
    badges: ["精选"],
    quality: "HD",
    author: "Trail Cam",
    views: 92110,
    publishedAt: "1 周前",
    sourceLabel: "夸克网盘",
  },
  {
    id: "p1-promo-3",
    mediaType: "video",
    href: "/video/p1-promo-3",
    title: "P1 精选 · 老电影修复画质对比",
    thumbnail: "/p/thumb/p1-promo-3.jpg",
    previewSrc: "/p/preview/p1-promo-3.mp4",
    previewDuration: 5,
    previewStrategy: "teaser-file",
    duration: "23:45",
    badges: ["精选", "修复"],
    quality: "HD",
    author: "Cinema Lab",
    views: 51200,
    publishedAt: "2 周前",
    sourceLabel: "115 网盘",
  },
  {
    id: "p1-promo-4",
    mediaType: "video",
    href: "/video/p1-promo-4",
    title: "P1 精选 · 星空延时 4K 60fps",
    thumbnail: "/p/thumb/p1-promo-4.jpg",
    previewSrc: "/p/preview/p1-promo-4.mp4",
    previewDuration: 5,
    previewStrategy: "teaser-file",
    duration: "15:02",
    badges: ["精选", "4K", "60fps"],
    quality: "HD",
    author: "Sky Watch",
    views: 246100,
    publishedAt: "5 天前",
    sourceLabel: "PikPak",
  },
  {
    id: "p1-promo-5",
    mediaType: "video",
    href: "/video/p1-promo-5",
    title: "P1 精选 · 街头美食纪录片",
    thumbnail: "/p/thumb/p1-promo-5.jpg",
    previewSrc: "/p/preview/p1-promo-5.mp4",
    previewDuration: 5,
    previewStrategy: "teaser-file",
    duration: "32:18",
    badges: ["精选", "纪录片"],
    quality: "HD",
    author: "Tasty",
    views: 142500,
    publishedAt: "1 周前",
    sourceLabel: "OneDrive",
  },
  {
    id: "p1-promo-6",
    mediaType: "video",
    href: "/video/p1-promo-6",
    title: "P1 精选 · 极光摄影教程",
    thumbnail: "/p/thumb/p1-promo-6.jpg",
    previewSrc: "/p/preview/p1-promo-6.mp4",
    previewDuration: 5,
    previewStrategy: "teaser-file",
    duration: "18:55",
    badges: ["精选", "教程"],
    quality: "HD",
    author: "Aurora Pro",
    views: 78340,
    publishedAt: "3 周前",
    sourceLabel: "联通网盘",
  },
  {
    id: "p1-promo-7",
    mediaType: "video",
    href: "/video/p1-promo-7",
    title: "P1 精选 · 城市建筑航拍合集",
    thumbnail: "/p/thumb/p1-promo-7.jpg",
    previewSrc: "/p/preview/p1-promo-7.mp4",
    previewDuration: 5,
    previewStrategy: "teaser-file",
    duration: "21:11",
    badges: ["精选", "航拍"],
    quality: "HD",
    author: "Sky Cam",
    views: 113200,
    publishedAt: "4 天前",
    sourceLabel: "夸克网盘",
  },
  {
    id: "p1-promo-8",
    mediaType: "video",
    href: "/video/p1-promo-8",
    title: "P1 精选 · 雨夜氛围白噪音",
    thumbnail: "/p/thumb/p1-promo-8.jpg",
    previewSrc: "/p/preview/p1-promo-8.mp4",
    previewDuration: 5,
    previewStrategy: "teaser-file",
    duration: "01:00:00",
    badges: ["精选", "氛围"],
    quality: "HD",
    author: "Ambience",
    views: 320100,
    publishedAt: "2 月前",
    sourceLabel: "本地",
  },
  {
    id: "p1-promo-9",
    mediaType: "video",
    href: "/video/p1-promo-9",
    title: "P1 精选 · 野生动物微距摄影",
    thumbnail: "/p/thumb/p1-promo-9.jpg",
    previewSrc: "/p/preview/p1-promo-9.mp4",
    previewDuration: 5,
    previewStrategy: "teaser-file",
    duration: "27:33",
    badges: ["精选", "微距"],
    quality: "HD",
    author: "Wild Lens",
    views: 96500,
    publishedAt: "1 月前",
    sourceLabel: "123 网盘",
  },
  {
    id: "p1-promo-10",
    mediaType: "video",
    href: "/video/p1-promo-10",
    title: "P1 精选 · 街头滑板 60fps 慢动作",
    thumbnail: "/p/thumb/p1-promo-10.jpg",
    previewSrc: "/p/preview/p1-promo-10.mp4",
    previewDuration: 5,
    previewStrategy: "teaser-file",
    duration: "06:48",
    badges: ["精选", "60fps"],
    quality: "HD",
    author: "Skate Co",
    views: 41020,
    publishedAt: "6 天前",
    sourceLabel: "光鸭云盘",
  },
];

export default PromoStrip;
