import { useEffect, useState, useCallback } from "react";
import { useParams, Link } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import { fetchGalleryDetail } from "@/data/galleries";
import type { GalleryDetail } from "@/types";
import { ArrowLeft, ChevronLeft, ChevronRight, Image } from "lucide-react";

export default function GalleryDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [detail, setDetail] = useState<GalleryDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [currentIndex, setCurrentIndex] = useState(0);

  useEffect(() => {
    if (!id) return;
    let active = true;
    setLoading(true);
    setError(null);
    fetchGalleryDetail(id).then((data) => {
      if (!active) return;
      if (!data) {
        setError("图集不存在或已被删除");
      } else {
        setDetail(data);
        document.title = `${data.title} · 图集 · 61`;
      }
      setLoading(false);
    });
    return () => {
      active = false;
    };
  }, [id]);

  const currentImage = detail?.images?.[currentIndex] ?? null;
  const totalImages = detail?.images?.length ?? 0;

  const goTo = useCallback(
    (index: number) => {
      if (index < 0) index = 0;
      if (totalImages > 0 && index >= totalImages) index = totalImages - 1;
      setCurrentIndex(index);
      // Scroll to top of image
      window.scrollTo({ top: 0, behavior: "smooth" });
    },
    [totalImages]
  );

  // Keyboard navigation
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowLeft") goTo(currentIndex - 1);
      if (e.key === "ArrowRight") goTo(currentIndex + 1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [currentIndex, goTo]);

  if (loading) {
    return (
      <AppShell>
        <div className="container page-section">
          <div className="gallery-detail-skeleton">
            <div className="skeleton skeleton--text" style={{ width: "60%", height: "1.5rem" }} />
            <div className="skeleton" style={{ width: "100%", aspectRatio: "16/9" }} />
          </div>
        </div>
      </AppShell>
    );
  }

  if (error || !detail) {
    return (
      <AppShell>
        <div className="container page-section">
          <div className="empty-state">
            <Image size={48} />
            <p>{error ?? "图集不存在"}</p>
            <Link to="/galleries" className="btn btn--primary" style={{ marginTop: "1rem" }}>
              返回图集列表
            </Link>
          </div>
        </div>
      </AppShell>
    );
  }

  return (
    <AppShell>
      <div className="container page-section">
        {/* Breadcrumb */}
        <div className="gallery-detail__breadcrumb">
          <Link to="/galleries" className="gallery-detail__back">
            <ArrowLeft size={16} />
            <span>图集列表</span>
          </Link>
        </div>

        {/* Header */}
        <div className="gallery-detail__header">
          <h1 className="gallery-detail__title">{detail.title}</h1>
          <div className="gallery-detail__meta">
            <span className="gallery-detail__author">{detail.author}</span>
            <span className="gallery-detail__count">{totalImages} 张图片</span>
          </div>
          {detail.tags.length > 0 && (
            <div className="gallery-detail__tags">
              {detail.tags.map((tag) => (
                <Link key={tag} to={`/galleries?tag=${encodeURIComponent(tag)}`} className="tag">
                  {tag}
                </Link>
              ))}
            </div>
          )}
          {detail.description && (
            <p className="gallery-detail__desc">{detail.description}</p>
          )}
        </div>

        {/* Image viewer */}
        <div className="gallery-viewer">
          {/* Navigation bar */}
          <div className="gallery-viewer__nav">
            <button
              className="gallery-viewer__btn"
              onClick={() => goTo(currentIndex - 1)}
              disabled={currentIndex <= 0}
              aria-label="上一张"
            >
              <ChevronLeft size={20} />
            </button>
            <span className="gallery-viewer__counter">
              {currentIndex + 1} / {totalImages}
            </span>
            <button
              className="gallery-viewer__btn"
              onClick={() => goTo(currentIndex + 1)}
              disabled={currentIndex >= totalImages - 1}
              aria-label="下一张"
            >
              <ChevronRight size={20} />
            </button>
          </div>

          {/* Current image */}
          {currentImage && (
            <div className="gallery-viewer__image-wrap">
              <img
                key={currentImage.url}
                src={currentImage.url}
                alt={`${detail.title} - 第 ${currentIndex + 1} 张`}
                className="gallery-viewer__image"
                onError={(e) => {
                  const el = e.target as HTMLImageElement;
                  el.style.display = "none";
                  const fb = el.nextElementSibling;
                  if (fb) fb.classList.remove("hidden");
                }}
              />
              <div className="gallery-viewer__image-fallback hidden">
                <Image size={64} />
                <p>图片加载失败</p>
              </div>
            </div>
          )}

          {/* Thumbnail strip */}
          {totalImages > 1 && (
            <div className="gallery-viewer__thumbs">
              {detail.images.map((img, idx) => (
                <button
                  key={img.position ?? idx}
                  className={`gallery-viewer__thumb ${idx === currentIndex ? "is-active" : ""}`}
                  onClick={() => goTo(idx)}
                  aria-label={`第 ${idx + 1} 张`}
                >
                  <img
                    src={img.thumbUrl || img.url}
                    alt={`第 ${idx + 1} 张`}
                    loading="lazy"
                  />
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
    </AppShell>
  );
}
