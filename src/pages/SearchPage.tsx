import { useEffect, useRef, useState, FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { BookOpen, ExternalLink, Film, Globe, Loader2, Search } from "lucide-react";
import { AppShell } from "@/components/AppShell";
import { fetchUnifiedSearch, type UnifiedSearchItem } from "@/data/videos";

const TYPE_LABELS: Record<string, string> = {
  video: "本地视频",
  novel: "小说",
  source: "搜索来源",
  resource: "资源站",
};

const TYPE_ICONS: Record<string, React.ReactNode> = {
  video: <Film size={15} />,
  novel: <BookOpen size={15} />,
  source: <Globe size={15} />,
  resource: <Globe size={15} />,
};

export default function SearchPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const q = params.get("q") ?? "";
  const [inputVal, setInputVal] = useState(q);
  const [items, setItems] = useState<UnifiedSearchItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    setInputVal(q);
    if (!q.trim()) {
      setItems([]);
      setSearched(false);
      return;
    }
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setLoading(true);
    setSearched(false);
    fetchUnifiedSearch(q)
      .then((res) => {
        if (!ctrl.signal.aborted) {
          setItems(res.items);
          setSearched(true);
        }
      })
      .catch(() => {
        if (!ctrl.signal.aborted) {
          setItems([]);
          setSearched(true);
        }
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoading(false);
      });
    return () => ctrl.abort();
  }, [q]);

  useEffect(() => {
    document.title = q ? `搜索 "${q}" · 61` : "搜索 · 61";
  }, [q]);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const kw = inputVal.trim();
    if (!kw) return;
    navigate(`/search?q=${encodeURIComponent(kw)}`);
  }

  // Group by type order
  const sections = (["video", "novel", "resource", "source"] as const).map((type) => ({
    type,
    items: items.filter((it) => it.type === type),
  })).filter((s) => s.items.length > 0);

  return (
    <AppShell>
      <div className="container page-section">
        <form className="search-panel" onSubmit={handleSubmit} role="search">
          <div className="search-panel__form">
            <div className="search-panel__input-wrapper">
              <Search size={16} className="search-panel__search-icon" />
              <input
                className="search-panel__input"
                type="text"
                value={inputVal}
                onChange={(e) => setInputVal(e.target.value)}
                placeholder="搜索视频、小说、资源站..."
                aria-label="搜索关键词"
                autoFocus
              />
            </div>
            <button className="search-panel__submit" type="submit">
              <Search size={16} className="search-panel__submit-icon" />
              <span className="search-panel__submit-text">搜索</span>
            </button>
          </div>
        </form>

        <div className="search-results">
          {loading && (
            <div className="search-results__spinner">
              <Loader2 size={28} className="spin" />
            </div>
          )}

          {!loading && searched && items.length === 0 && (
            <div className="search-results__empty">
              <p>没有找到与 "{q}" 相关的结果</p>
            </div>
          )}

          {!loading && searched && items.length > 0 && (
            <>
              <div className="search-results__header">
                <h1 className="search-results__query">"{q}"</h1>
                <p className="search-results__meta">共 {items.length} 条结果</p>
              </div>

              {sections.map(({ type, items: sectionItems }) => (
                <section key={type} className="search-results__section">
                  <h2 className="search-results__section-title">
                    {TYPE_ICONS[type]}
                    {TYPE_LABELS[type]}
                    <span className="search-results__meta">（{sectionItems.length}）</span>
                  </h2>
                  <ul className="search-results__list">
                    {sectionItems.map((item) => (
                      <SearchResultItem key={item.id} item={item} />
                    ))}
                  </ul>
                </section>
              ))}
            </>
          )}
        </div>
      </div>
    </AppShell>
  );
}

function SearchResultItem({ item }: { item: UnifiedSearchItem }) {
  const isExternal = item.type === "source" || (item.type === "resource" && !item.href);

  const inner = (
    <>
      <div className="search-result-item__thumb">
        {item.cover ? (
          <img src={item.cover} alt={item.title} loading="lazy" />
        ) : (
          <div className="search-result-item__thumb-placeholder">
            {TYPE_ICONS[item.type]}
          </div>
        )}
      </div>
      <div className="search-result-item__body">
        <div className="search-result-item__title">{item.title}</div>
        {item.subtitle && (
          <div className="search-result-item__subtitle">{item.subtitle}</div>
        )}
      </div>
      {item.directPlay && (
        <span className="search-result-item__badge search-result-item__badge--direct">
          直链
        </span>
      )}
      {isExternal && (
        <ExternalLink size={14} style={{ color: "var(--text-faint)", flexShrink: 0 }} />
      )}
    </>
  );

  if (isExternal && item.url) {
    return (
      <li>
        <a
          className="search-result-item"
          href={item.url}
          target="_blank"
          rel="noopener noreferrer"
        >
          {inner}
        </a>
      </li>
    );
  }

  const href = item.href ?? (item.type === "video" ? `/video/${item.id}` : item.type === "novel" ? `/novel/${item.id}` : "#");

  return (
    <li>
      <a className="search-result-item" href={href}>
        {inner}
      </a>
    </li>
  );
}
