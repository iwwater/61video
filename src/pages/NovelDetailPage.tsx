import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import { PdfViewer } from "@/components/PdfViewer";
import { fetchNovelDetail } from "@/data/novels";
import type { NovelChapter, NovelDetail } from "@/types";
import {
  ArrowLeft,
  ArrowRight,
  BookOpen,
  ChevronLeft,
  Menu as MenuIcon,
  Settings as SettingsIcon,
  Type,
  X,
} from "lucide-react";

const READER_STATE_PREFIX = "video-site:novel-reader:";
const READER_PREFS_PREFIX = "video-site:novel-prefs:";

type Theme = "light" | "sepia" | "dark" | "eyecare" | "green";
type FontFamily = "system" | "serif" | "sans";

type ReaderPrefs = {
  theme: Theme;
  fontSize: number; // 14-28
  lineHeight: number; // 1.4-2.4 (×100 for storage)
  fontFamily: FontFamily;
  maxWidth: number; // 600-900
};

const DEFAULT_PREFS: ReaderPrefs = {
  theme: "sepia",
  fontSize: 18,
  lineHeight: 1.8,
  fontFamily: "serif",
  maxWidth: 720,
};

const THEMES: Record<Theme, { bg: string; fg: string; sub: string }> = {
  light: { bg: "#ffffff", fg: "#1a1a1a", sub: "#666" },
  sepia: { bg: "#f4ecd8", fg: "#5b4636", sub: "#8b7355" },
  dark: { bg: "#1a1a1a", fg: "#d0d0d0", sub: "#888" },
  eyecare: { bg: "#c7e0c5", fg: "#2a3a2a", sub: "#5a6a5a" },
  green: { bg: "#3a4a3a", fg: "#d0d8d0", sub: "#9aa89a" },
};

export default function NovelDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const [detail, setDetail] = useState<NovelDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [activeIdx, setActiveIdx] = useState<number>(0);
  const [chapter, setChapter] = useState<NovelChapter | null>(null);
  const [showSettings, setShowSettings] = useState(false);
  const [showToc, setShowToc] = useState(false);
  const [barsVisible, setBarsVisible] = useState(true);
  const hideBarsTimer = useRef<number | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const [prefs, setPrefs] = useState<ReaderPrefs>(() => readPrefs(id));

  useEffect(() => {
    document.title = detail ? `${detail.title} · 61` : "小说 · 61";
  }, [detail]);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setDetail(null);
    setChapter(null);
    fetchNovelDetail(id).then((d) => {
      if (!active) return;
      setDetail(d);
      setLoading(false);
      if (d) {
        const saved = readSavedPosition(id);
        if (saved !== null && saved >= 0 && saved < d.chapters.length) {
          setActiveIdx(saved);
        } else {
          setActiveIdx(0);
        }
      }
    });
    return () => {
      active = false;
    };
  }, [id]);

  useEffect(() => {
    if (!detail) return;
    const ch = detail.chapters[activeIdx];
    if (!ch) return;
    if (ch.contentType === "pdf") {
      setChapter(ch);
      return;
    }
    if (ch.body && ch.body.length > 0) {
      setChapter(ch);
      return;
    }
    let active = true;
    setChapter(null);
    import("@/data/novels").then(({ fetchNovelChapter }) => {
      fetchNovelChapter(id, ch.position).then((c) => {
        if (!active) return;
        setChapter(c);
      });
    });
    return () => {
      active = false;
    };
  }, [detail, activeIdx, id]);

  // 持久化阅读位置 + 读者偏好
  useEffect(() => {
    if (!detail) return;
    writeSavedPosition(id, activeIdx);
  }, [id, detail, activeIdx]);

  useEffect(() => {
    writePrefs(id, prefs);
  }, [id, prefs]);

  // 切章节时滚动到顶部
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTo({ top: 0 });
    }
  }, [activeIdx]);

  // 自动隐藏工具栏（5s 无操作）
  useEffect(() => {
    if (!barsVisible) return;
    if (hideBarsTimer.current) window.clearTimeout(hideBarsTimer.current);
    hideBarsTimer.current = window.setTimeout(() => {
      setBarsVisible(false);
    }, 5000);
    return () => {
      if (hideBarsTimer.current) window.clearTimeout(hideBarsTimer.current);
    };
  }, [barsVisible, activeIdx, showSettings, showToc]);

  const chapterCount = detail?.chapters.length ?? 0;
  const goPrev = () => setActiveIdx((i) => Math.max(0, i - 1));
  const goNext = () =>
    setActiveIdx((i) => Math.min(Math.max(0, chapterCount - 1), i + 1));

  const theme = THEMES[prefs.theme];

  if (loading) {
    return (
      <AppShell>
        <div className="container page-section">
          <div className="skeleton" style={{ height: 320 }} />
        </div>
      </AppShell>
    );
  }

  if (!detail) {
    return (
      <AppShell>
        <div className="container page-section">
          <div className="empty-state">
            <BookOpen size={48} />
            <p>未找到该小说</p>
            <Link to="/novels" className="button button--ghost">
              <ChevronLeft size={16} /> 返回小说列表
            </Link>
          </div>
        </div>
      </AppShell>
    );
  }

  return (
    <div
      className="novel-reader-page"
      style={{
        "--reader-bg": theme.bg,
        "--reader-fg": theme.fg,
        "--reader-sub": theme.sub,
      } as React.CSSProperties}
    >
      {/* 顶部栏 */}
      <header className={`reader-topbar ${barsVisible ? "" : "is-hidden"}`}>
        <button
          type="button"
          className="reader-iconbtn"
          onClick={() => window.history.back()}
          aria-label="返回"
        >
          <ChevronLeft size={22} />
        </button>
        <div className="reader-topbar__title">
          <span className="reader-topbar__book">{detail.title}</span>
          <span className="reader-topbar__author">· {detail.author}</span>
        </div>
        <div className="reader-topbar__actions">
          <button
            type="button"
            className="reader-iconbtn"
            onClick={() => setShowToc((v) => !v)}
            aria-label="目录"
          >
            <MenuIcon size={22} />
          </button>
          <button
            type="button"
            className="reader-iconbtn"
            onClick={() => setShowSettings((v) => !v)}
            aria-label="设置"
          >
            <SettingsIcon size={22} />
          </button>
        </div>
      </header>

      {/* 阅读区 */}
      <main
        ref={scrollRef}
        className="reader-scroll"
        onClick={() => {
          setBarsVisible((v) => !v);
          setShowSettings(false);
          setShowToc(false);
        }}
      >
        <article
          className={`reader-content reader-font-${prefs.fontFamily}`}
          style={{
            fontSize: `${prefs.fontSize}px`,
            lineHeight: prefs.lineHeight,
            maxWidth: `${prefs.maxWidth}px`,
          }}
        >
          {chapter ? (
            <>
              <h1 className="reader-content__title">
                {chapter.title || `第 ${activeIdx + 1} 章`}
              </h1>
              {chapter.contentType === "pdf" ? (
                <PdfViewer
                  file={chapter.pdfUrl || ""}
                  title={chapter.title || `第 ${activeIdx + 1} 章`}
                />
              ) : (
                <div
                  className="reader-content__body"
                  dangerouslySetInnerHTML={{ __html: chapter.body || "" }}
                />
              )}
              <div className="reader-content__end">— 本章完 —</div>
            </>
          ) : (
            <div className="skeleton" style={{ height: 320 }} />
          )}
        </article>
      </main>

      {/* 底部栏 */}
      <footer className={`reader-bottombar ${barsVisible ? "" : "is-hidden"}`}>
        <button
          type="button"
          className="reader-pill"
          onClick={goPrev}
          disabled={activeIdx === 0}
        >
          <ArrowLeft size={16} /> 上一章
        </button>
        <span className="reader-progress">
          {activeIdx + 1} / {chapterCount}
        </span>
        <button
          type="button"
          className="reader-pill reader-pill--primary"
          onClick={goNext}
          disabled={activeIdx >= chapterCount - 1}
        >
          下一章 <ArrowRight size={16} />
        </button>
      </footer>

      {/* 目录抽屉 */}
      {showToc ? (
        <TocDrawer
          chapters={detail.chapters}
          activeIdx={activeIdx}
          onPick={(i) => {
            setActiveIdx(i);
            setShowToc(false);
            setBarsVisible(true);
          }}
          onClose={() => setShowToc(false)}
        />
      ) : null}

      {/* 设置面板 */}
      {showSettings ? (
        <SettingsPanel
          prefs={prefs}
          onChange={(p) => setPrefs(p)}
          onClose={() => setShowSettings(false)}
        />
      ) : null}
    </div>
  );
}

function TocDrawer({
  chapters,
  activeIdx,
  onPick,
  onClose,
}: {
  chapters: NovelChapter[];
  activeIdx: number;
  onPick: (idx: number) => void;
  onClose: () => void;
}) {
  const [kw, setKw] = useState("");
  const filtered = useMemo(
    () =>
      kw
        ? chapters.filter((c) => c.title.toLowerCase().includes(kw.toLowerCase()))
        : chapters,
    [chapters, kw]
  );
  return (
    <aside className="reader-drawer" role="dialog">
      <div className="reader-drawer__head">
        <h3>章节目录 · {chapters.length}</h3>
        <button
          type="button"
          className="reader-iconbtn"
          onClick={onClose}
          aria-label="关闭"
        >
          <X size={20} />
        </button>
      </div>
      <div className="reader-drawer__search">
        <input
          type="search"
          placeholder="搜索章节"
          value={kw}
          onChange={(e) => setKw(e.target.value)}
        />
      </div>
      <ol className="reader-toc">
        {filtered.map((ch) => {
          const realIdx = chapters.findIndex((c) => c.id === ch.id);
          return (
            <li key={ch.id}>
              <button
                type="button"
                className={`reader-toc__item ${
                  realIdx === activeIdx ? "is-active" : ""
                }`}
                onClick={() => onPick(realIdx)}
              >
                <span className="reader-toc__pos">
                  {String(realIdx + 1).padStart(3, "0")}
                </span>
                <span className="reader-toc__name">
                  {ch.title || `第 ${realIdx + 1} 章`}
                </span>
              </button>
            </li>
          );
        })}
      </ol>
    </aside>
  );
}

function SettingsPanel({
  prefs,
  onChange,
  onClose,
}: {
  prefs: ReaderPrefs;
  onChange: (p: ReaderPrefs) => void;
  onClose: () => void;
}) {
  return (
    <div className="reader-settings" role="dialog">
      <div className="reader-settings__head">
        <h3>
          <Type size={18} /> 阅读偏好
        </h3>
        <button
          type="button"
          className="reader-iconbtn"
          onClick={onClose}
          aria-label="关闭"
        >
          <X size={20} />
        </button>
      </div>

      <div className="reader-settings__group">
        <label>主题</label>
        <div className="reader-theme-row">
          {(Object.keys(THEMES) as Theme[]).map((t) => (
            <button
              key={t}
              type="button"
              className={`reader-theme-chip ${
                prefs.theme === t ? "is-active" : ""
              }`}
              style={{
                background: THEMES[t].bg,
                color: THEMES[t].fg,
                borderColor: prefs.theme === t ? THEMES[t].fg : "transparent",
              }}
              onClick={() => onChange({ ...prefs, theme: t })}
            >
              {t === "light"
                ? "白"
                : t === "sepia"
                ? "米"
                : t === "dark"
                ? "黑"
                : t === "eyecare"
                ? "护眼"
                : "绿"}
            </button>
          ))}
        </div>
      </div>

      <div className="reader-settings__group">
        <label>字号 ({prefs.fontSize}px)</label>
        <input
          type="range"
          min={14}
          max={28}
          value={prefs.fontSize}
          onChange={(e) =>
            onChange({ ...prefs, fontSize: Number(e.target.value) })
          }
        />
      </div>

      <div className="reader-settings__group">
        <label>行距 ({prefs.lineHeight.toFixed(2)})</label>
        <input
          type="range"
          min={140}
          max={240}
          value={Math.round(prefs.lineHeight * 100)}
          onChange={(e) =>
            onChange({ ...prefs, lineHeight: Number(e.target.value) / 100 })
          }
        />
      </div>

      <div className="reader-settings__group">
        <label>页面宽度 ({prefs.maxWidth}px)</label>
        <input
          type="range"
          min={600}
          max={900}
          step={20}
          value={prefs.maxWidth}
          onChange={(e) =>
            onChange({ ...prefs, maxWidth: Number(e.target.value) })
          }
        />
      </div>

      <div className="reader-settings__group">
        <label>字体</label>
        <div className="reader-font-row">
          {(["serif", "sans", "system"] as FontFamily[]).map((f) => (
            <button
              key={f}
              type="button"
              className={`reader-font-chip ${
                prefs.fontFamily === f ? "is-active" : ""
              }`}
              onClick={() => onChange({ ...prefs, fontFamily: f })}
            >
              {f === "serif" ? "宋体" : f === "sans" ? "黑体" : "系统"}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

// ---------- 持久化 ----------

function readSavedPosition(id: string): number | null {
  try {
    const raw = window.localStorage.getItem(READER_STATE_PREFIX + id);
    if (!raw) return null;
    const n = Number.parseInt(raw, 10);
    return Number.isFinite(n) && n >= 0 ? n : null;
  } catch {
    return null;
  }
}

function writeSavedPosition(id: string, pos: number) {
  try {
    window.localStorage.setItem(READER_STATE_PREFIX + id, String(pos));
  } catch {
    // ignore
  }
}

function readPrefs(id: string): ReaderPrefs {
  try {
    const raw = window.localStorage.getItem(READER_PREFS_PREFIX + id);
    if (!raw) return DEFAULT_PREFS;
    const obj = JSON.parse(raw) as Partial<ReaderPrefs>;
    return { ...DEFAULT_PREFS, ...obj };
  } catch {
    return DEFAULT_PREFS;
  }
}

function writePrefs(id: string, p: ReaderPrefs) {
  try {
    window.localStorage.setItem(READER_PREFS_PREFIX + id, JSON.stringify(p));
  } catch {
    // ignore
  }
}
