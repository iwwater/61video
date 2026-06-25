import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import {
  buildIframeUrl,
  getResourceDetail,
  listAnimeSources,
  parseAnimeUrl,
  searchAnime,
  type AnimeSearchItem,
  type AnimeSearchResult,
  type AnimeSource,
} from "@/data/anime";
import { VideoPlayer } from "@/components/VideoPlayer";
import {
  ChevronDown,
  ExternalLink,
  Film,
  History as HistoryIcon,
  Loader2,
  Play,
  Search as SearchIcon,
  Sparkles,
  Trash2,
  Tv,
  Wand2,
  X,
} from "lucide-react";

const HISTORY_KEY = "video-site:parse-history";
const RECENT_KEY = "video-site:parse-recent";
const HOT_TAGS = [
  "庆余年",
  "鬼灭之刃",
  "间谍过家家",
  "凡人修仙传",
  "长相思",
  "斗罗大陆",
  "三体",
  "进击的巨人",
];
const MAX_HISTORY = 12;
const MAX_RECENT = 8;

type HistoryEntry = {
  url: string;
  title: string;
  thumbnail?: string;
  source: string;
  sourceId: string;
  sourceName: string;
  isIframe: boolean;
  ts: number;
};

type View = "search" | "parse";

export default function AnimeParsePage() {
  const [tab, setTab] = useState<View>("search");
  const [kw, setKw] = useState("");
  const [url, setUrl] = useState("");
  const [selectedSourceId, setSelectedSourceId] = useState<string>("universal");
  const [searching, setSearching] = useState(false);
  const [parsing, setParsing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [results, setResults] = useState<AnimeSearchResult | null>(null);
  const [sources, setSources] = useState<AnimeSource[]>([]);
  const [player, setPlayer] = useState<{
    url: string;
    title: string;
    thumbnail?: string;
  } | null>(null);
  const [iframe, setIframe] = useState<{
    url: string;
    title: string;
    sourceName: string;
  } | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [recent, setRecent] = useState<string[]>([]);
  const [params] = useSearchParams();
  const inputRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();

  useEffect(() => {
    document.title = "影视搜索+解析 · 61";
  }, []);

  useEffect(() => {
    listAnimeSources().then(setSources).catch(() => setSources([]));
    setHistory(readHistory());
    setRecent(readRecent());
  }, []);

  // 支持 ?parse=URL 或 ?kw=keyword 初始填入
  useEffect(() => {
    const initUrl = params.get("parse");
    const initKw = params.get("kw");
    if (initUrl) {
      setUrl(initUrl);
      setTab("parse");
      setTimeout(() => handleParseWith(initUrl), 0);
    } else if (initKw) {
      setKw(initKw);
      setTab("search");
      setTimeout(() => handleSearchWith(initKw), 0);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);

  const onSearch = async (e?: React.FormEvent) => {
    e?.preventDefault();
    return handleSearchWith(kw.trim());
  };

  const onParse = async (e?: React.FormEvent) => {
    e?.preventDefault();
    return handleParseWith(url.trim());
  };

  async function handleSearchWith(q: string) {
    if (!q) return;
    setSearching(true);
    setError(null);
    setResults(null);
    pushRecent(q);
    setRecent(readRecent());
    try {
      const r = await searchAnime(q);
      setResults(r);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSearching(false);
    }
  }

  async function handleParseWith(u: string) {
    if (!u) return;
    if (!/^https?:\/\//i.test(u)) {
      setError("请输入 http(s) 开头的 URL");
      return;
    }
    setParsing(true);
    setError(null);
    setPlayer(null);
    setIframe(null);
    const src = sources.find((s) => s.id === selectedSourceId);
    const isIframe = src?.isIframe ?? false;
    const sourceName = src?.name ?? "未知";
    try {
      if (isIframe) {
        const r = await buildIframeUrl(selectedSourceId, u);
        setIframe({ url: r.url, title: u, sourceName });
        pushHistory({
          url: u,
          title: u,
          source: r.source,
          sourceId: selectedSourceId,
          sourceName: r.name,
          isIframe: true,
          ts: Date.now(),
        });
        setHistory(readHistory());
      } else {
        const r = await parseAnimeUrl(u);
        setPlayer({
          url: r.videoUrl,
          title: r.title || u,
          thumbnail: r.thumbnail,
        });
        pushHistory({
          url: u,
          title: r.title || u,
          thumbnail: r.thumbnail,
          source: r.source || "universal",
          sourceId: selectedSourceId,
          sourceName,
          isIframe: false,
          ts: Date.now(),
        });
        setHistory(readHistory());
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setParsing(false);
    }
  }

  function clearRecent() {
    writeRecent([]);
    setRecent([]);
  }

  function clearHistory() {
    writeHistory([]);
    setHistory([]);
  }

  const selectedSource = sources.find((s) => s.id === selectedSourceId);

  return (
    <AppShell>
      <div className="parse-page">
        {/* Hero */}
        <section className="parse-hero">
          <div className="parse-hero__bg" aria-hidden />
          <div className="container parse-hero__inner">
            <h1 className="parse-hero__title">
              <Film size={32} /> 影视搜索 · 在线解析 · 多源聚合
            </h1>
            <p className="parse-hero__sub">
              搜剧名查本地库或外站搜索页，粘视频链接秒级抽 m3u8 / mp4
            </p>

            <div className="parse-tabs" role="tablist">
              <button
                role="tab"
                aria-selected={tab === "search"}
                className={`parse-tab ${tab === "search" ? "is-active" : ""}`}
                onClick={() => setTab("search")}
              >
                <SearchIcon size={16} /> 搜剧名
              </button>
              <button
                role="tab"
                aria-selected={tab === "parse"}
                className={`parse-tab ${tab === "parse" ? "is-active" : ""}`}
                onClick={() => setTab("parse")}
              >
                <Wand2 size={16} /> 解析链接
              </button>
            </div>

            {tab === "search" ? (
              <form className="parse-search" onSubmit={onSearch}>
                <SearchIcon size={20} className="parse-search__icon" />
                <input
                  ref={inputRef}
                  type="search"
                  className="parse-search__input"
                  placeholder="搜剧名 / 关键词（如 庆余年 / 鬼灭之刃）"
                  value={kw}
                  onChange={(e) => setKw(e.target.value)}
                  disabled={searching}
                  autoFocus
                />
                <button
                  type="submit"
                  className="parse-search__btn"
                  disabled={searching || !kw.trim()}
                >
                  {searching ? (
                    <Loader2 size={18} className="spin" />
                  ) : (
                    <SearchIcon size={18} />
                  )}
                  {searching ? "搜索中…" : "搜索"}
                </button>
              </form>
            ) : (
              <form className="parse-search" onSubmit={onParse}>
                <Wand2 size={20} className="parse-search__icon" />
                <input
                  type="url"
                  className="parse-search__input"
                  placeholder="粘视频页面 URL，如 https://example.com/play/12345"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  disabled={parsing}
                />
                <button
                  type="submit"
                  className="parse-search__btn"
                  disabled={parsing || !url.trim()}
                >
                  {parsing ? <Loader2 size={18} className="spin" /> : <Play size={18} />}
                  {parsing ? "解析中…" : "立即解析"}
                </button>
              </form>
            )}

            {/* 源选择器（解析时用） */}
            {tab === "parse" ? (
              <div className="parse-source-picker">
                <label htmlFor="source-select">解析源：</label>
                <div className="parse-source-select">
                  <select
                    id="source-select"
                    value={selectedSourceId}
                    onChange={(e) => setSelectedSourceId(e.target.value)}
                    disabled={parsing}
                  >
                    {sources.length === 0 ? (
                      <option value="universal">通用（HTML 兜底）</option>
                    ) : (
                      sources.map((s) => (
                        <option key={s.id} value={s.id}>
                          {s.name}
                          {s.isIframe ? " · iframe" : ""}
                          {s.canSearch ? " · 搜" : ""}
                        </option>
                      ))
                    )}
                  </select>
                  <ChevronDown size={14} />
                </div>
                {selectedSource?.isIframe ? (
                  <span className="parse-source-picker__hint">
                    此源返回带 player 的 HTML 页面，将用 iframe 嵌入
                  </span>
                ) : null}
              </div>
            ) : null}

            {/* 热门标签 */}
            <div className="parse-hot">
              <Sparkles size={14} />
              <span>热门：</span>
              {HOT_TAGS.map((t) => (
                <button
                  key={t}
                  type="button"
                  className="parse-hot__chip"
                  onClick={() => {
                    setTab("search");
                    setKw(t);
                    handleSearchWith(t);
                  }}
                >
                  {t}
                </button>
              ))}
            </div>

            {/* 源 chips */}
            {sources.length > 0 ? (
              <div className="parse-sources">
                <Tv size={14} />
                <span>当前 {sources.length} 个源：</span>
                {sources.slice(0, 12).map((s) => (
                  <span
                    key={s.id}
                    className={`parse-source-chip ${
                      s.isIframe ? "is-iframe" : ""
                    }`}
                    title={s.note || ""}
                  >
                    {s.name}
                    {s.isIframe ? "·iframe" : ""}
                  </span>
                ))}
                {sources.length > 12 ? (
                  <span className="parse-source-chip">+{sources.length - 12}</span>
                ) : null}
              </div>
            ) : null}
          </div>
        </section>

        <div className="container parse-page__body">
          {error ? (
            <div className="parse-banner parse-banner--error">
              <strong>失败：</strong>
              <span>{error}</span>
              <button
                type="button"
                onClick={() => setError(null)}
                aria-label="关闭"
              >
                <X size={14} />
              </button>
            </div>
          ) : null}

          {player ? (
            <section className="parse-player-card">
              <div className="parse-player-card__head">
                <h2>
                  <Play size={18} /> {player.title}
                </h2>
                <button
                  type="button"
                  className="parse-iconbtn"
                  onClick={() => setPlayer(null)}
                  aria-label="关闭播放器"
                >
                  <X size={18} />
                </button>
              </div>
              <div className="parse-player-card__body">
                <VideoPlayer
                  src={player.url}
                  poster={player.thumbnail ?? ""}
                  title={player.title}
                />
              </div>
            </section>
          ) : null}

          {iframe ? (
            <section className="parse-player-card">
              <div className="parse-player-card__head">
                <h2>
                  <ExternalLink size={18} /> {iframe.title}
                  <span className="parse-player-card__source">
                    via {iframe.sourceName}
                  </span>
                </h2>
                <div style={{ display: "flex", gap: 4 }}>
                  <a
                    href={iframe.url}
                    target="_blank"
                    rel="noreferrer"
                    className="parse-iconbtn"
                    aria-label="新窗口打开"
                    title="新窗口打开"
                  >
                    <ExternalLink size={16} />
                  </a>
                  <button
                    type="button"
                    className="parse-iconbtn"
                    onClick={() => setIframe(null)}
                    aria-label="关闭"
                  >
                    <X size={18} />
                  </button>
                </div>
              </div>
              <div className="parse-player-card__body">
                <iframe
                  className="parse-iframe"
                  src={iframe.url}
                  title={iframe.title}
                  allow="autoplay; encrypted-media; fullscreen"
                  allowFullScreen
                  referrerPolicy="no-referrer"
                />
              </div>
            </section>
          ) : null}

          {tab === "search" && results ? (
            <SearchResults
              results={results}
              onPickLocal={(item) => {
                if (item.href) navigate(item.href);
              }}
              onPickSource={(item) => {
                if (item.url) {
                  // 在新标签页打开外站搜索结果页。失败时（被浏览器拦截）回退到
                  // 当前页面拉起用户手动点击。
                  const win = window.open(item.url, "_blank", "noopener,noreferrer");
                  if (!win) {
                    setError("浏览器拦截了新窗口，请允许弹窗后重试");
                  }
                }
              }}
              onPickResource={async (item) => {
                if (!item.siteId || !item.vodId) return;
                setError(null);
                try {
                  // 列表里 vod_play_url 经常为空（行业惯例），需要后端走详情接口拿真实 m3u8
                  const detail = await getResourceDetail(item.siteId, item.vodId);
                  if (detail.directPlay) {
                    // m3u8/mp4 直链：直接喂给项目自带播放器（黑底主题）
                    setError(null);
                    setIframe(null);
                    setPlayer({
                      url: detail.playUrl,
                      title: detail.title || item.title,
                      thumbnail: detail.cover || item.cover,
                    });
                    pushHistory({
                      url: detail.playUrl,
                      title: detail.title || item.title,
                      thumbnail: detail.cover || item.cover,
                      source: detail.siteName,
                      sourceId: item.siteId,
                      sourceName: detail.siteName,
                      isIframe: false,
                      ts: Date.now(),
                    });
                    setHistory(readHistory());
                    return;
                  }
                  // 非直链：playUrl 是资源站自家播放页（jszy 是 DPlayer 动态建 video）。
                  // 走 universal 解析器：抓页面 HTML → 提取里面的 m3u8（扫 JS 字符串
                  // 里的 DPlayer video.url 等模式）→ 拿真 m3u8 喂给项目自带播放器。
                  // 这样既不用 iframe 嵌入（避免白底主题冲突），也不用解析源（fongmi
                  // 那种是给官方平台 iqiyi/youku 用的，跟资源站不是一条链路）。
                  const parsed = await parseAnimeUrl(detail.playUrl);
                  setError(null);
                  setIframe(null);
                  setPlayer({
                    url: parsed.videoUrl,
                    title: parsed.title || detail.title || item.title,
                    thumbnail: parsed.thumbnail || detail.cover || item.cover,
                  });
                  pushHistory({
                    url: detail.playUrl,
                    title: parsed.title || detail.title || item.title,
                    thumbnail: parsed.thumbnail || detail.cover || item.cover,
                    source: detail.siteName,
                    sourceId: item.siteId,
                    sourceName: detail.siteName,
                    isIframe: false,
                    ts: Date.now(),
                  });
                  setHistory(readHistory());
                } catch (err) {
                  setError(
                    `拉取/解析失败：${err instanceof Error ? err.message : String(err)}`
                  );
                }
              }}
            />
          ) : null}

          {tab === "search" && !results && !searching ? (
            <EmptyState
              recent={recent}
              history={history}
              onPickRecent={(q) => {
                setKw(q);
                handleSearchWith(q);
              }}
              onPickHistory={(h) => {
                setUrl(h.url);
                setSelectedSourceId(h.sourceId);
                setTab("parse");
                handleParseWith(h.url);
              }}
              onClearRecent={clearRecent}
              onClearHistory={clearHistory}
            />
          ) : null}
        </div>
      </div>
    </AppShell>
  );
}

function SearchResults({
  results,
  onPickLocal,
  onPickSource,
  onPickResource,
}: {
  results: AnimeSearchResult;
  onPickLocal: (item: AnimeSearchItem) => void;
  onPickSource: (item: AnimeSearchItem) => void;
  onPickResource: (item: AnimeSearchItem) => void;
}) {
  const hasSource = results.items.some((i) => i.type === "source");
  const hasResource = results.items.some((i) => i.type === "resource");
  if (results.items.length === 0) {
    return (
      <section className="parse-empty">
        <h3>没有匹配的结果</h3>
        <p>
          本地库和已配置的 {results.remoteCount === 0 ? "（外站）" : ""} 解析源都没找到「{results.query}」。
        </p>
        <ul className="parse-empty__tips">
          <li>换个更短/更精确的关键词试试（如"庆余年 1"而不是"庆余年第一集 完整版 高清"）</li>
          <li>切到"解析链接" tab，粘视频页面 URL 直接用 {results.remoteCount} 个解析源播放</li>
          <li>去后台 → 系统 → 资源站，添加影视资源站（行业标准 JSON API）</li>
          <li>先上传几部本地视频到 /upload，搜索时会优先匹配</li>
        </ul>
      </section>
    );
  }
  return (
    <section className="parse-results">
      <div className="parse-results__meta">
        关键词「<strong>{results.query}</strong>」 · 本地 {results.localCount} ·
        外站 {results.remoteCount}
      </div>
      <div className="parse-grid">
        {results.items.map((item) => (
          <ResultCard
            key={`${item.type}-${item.id}-${item.url ?? ""}`}
            item={item}
            onPick={
              item.type === "source"
                ? () => onPickSource(item)
                : item.type === "resource"
                ? () => onPickResource(item)
                : () => onPickLocal(item)
            }
          />
        ))}
      </div>
      {hasSource ? (
        <p className="parse-results__hint">
          外站源已在新标签页打开。从外站搜索结果页点开某个视频的详情/播放页，
          复制 URL 后回到「解析链接」tab 粘贴即可解析。
        </p>
      ) : null}
      {hasResource ? (
        <p className="parse-results__hint">
          资源站结果：直链可点直接播放，详情页会自动用当前解析源解析。
        </p>
      ) : null}
    </section>
  );
}

function ResultCard({
  item,
  onPick,
}: {
  item: AnimeSearchItem;
  onPick: () => void;
}) {
  // source 类型：渲染为真 <a>，新标签页打开外站搜索结果页。
  if (item.type === "source") {
    return (
      <a
        className="parse-card parse-card--source"
        href={item.url}
        target="_blank"
        rel="noopener noreferrer"
        onClick={onPick}
      >
        <div className="parse-card__cover">
          <div className="parse-card__cover-fallback">🔗</div>
          <span className="parse-card__type parse-card__type--source">外站</span>
          <span className="parse-card__external">
            <ExternalLink size={14} />
          </span>
        </div>
        <div className="parse-card__body">
          <h3 className="parse-card__title" title={item.title}>
            {item.title}
          </h3>
          {item.subtitle ? (
            <p className="parse-card__sub">{item.subtitle}</p>
          ) : null}
          {item.source ? (
            <span className="parse-card__source">
              {item.source} · 新标签页打开
            </span>
          ) : null}
        </div>
      </a>
    );
  }
  // resource 类型：来自资源站（行业标准 JSON API），可能有封面图
  if (item.type === "resource") {
    return (
      <button
        type="button"
        className="parse-card parse-card--resource"
        onClick={onPick}
      >
        <div className="parse-card__cover">
          {item.cover ? (
            <img
              src={item.cover}
              alt={item.title}
              loading="lazy"
              onError={(e) => {
                (e.target as HTMLImageElement).style.display = "none";
              }}
            />
          ) : (
            <div className="parse-card__cover-fallback">🎬</div>
          )}
          <span className="parse-card__type parse-card__type--resource">
            {item.directPlay ? "直链" : "资源站"}
          </span>
          {item.directPlay ? (
            <span className="parse-card__play">
              <Play size={12} />
            </span>
          ) : null}
        </div>
        <div className="parse-card__body">
          <h3 className="parse-card__title" title={item.title}>
            {item.title}
          </h3>
          {item.subtitle ? (
            <p className="parse-card__sub">{item.subtitle}</p>
          ) : null}
          {item.source ? (
            <span className="parse-card__source">{item.source}</span>
          ) : null}
        </div>
      </button>
    );
  }
  return (
    <button type="button" className="parse-card" onClick={onPick}>
      <div className="parse-card__cover">
        {item.cover ? (
          <img
            src={item.cover}
            alt={item.title}
            loading="lazy"
            onError={(e) => {
              (e.target as HTMLImageElement).style.display = "none";
            }}
          />
        ) : (
          <div className="parse-card__cover-fallback">
            {item.type === "novel" ? "📖" : "🎬"}
          </div>
        )}
        <span
          className={`parse-card__type parse-card__type--${item.type}`}
        >
          {item.type === "video" ? "视频" : "小说"}
        </span>
      </div>
      <div className="parse-card__body">
        <h3 className="parse-card__title" title={item.title}>
          {item.title}
        </h3>
        {item.subtitle ? (
          <p className="parse-card__sub">{item.subtitle}</p>
        ) : null}
        {item.source ? (
          <span className="parse-card__source">{item.source}</span>
        ) : null}
      </div>
    </button>
  );
}

function EmptyState({
  recent,
  history,
  onPickRecent,
  onPickHistory,
  onClearRecent,
  onClearHistory,
}: {
  recent: string[];
  history: HistoryEntry[];
  onPickRecent: (q: string) => void;
  onPickHistory: (h: HistoryEntry) => void;
  onClearRecent: () => void;
  onClearHistory: () => void;
}) {
  if (recent.length === 0 && history.length === 0) {
    return (
      <section className="parse-empty">
        <h3>开始你的第一次搜索</h3>
        <p>在上面的搜索框输入剧名，或点击热门标签试试</p>
      </section>
    );
  }
  return (
    <div className="parse-side">
      {recent.length > 0 ? (
        <section className="parse-panel">
          <header>
            <h3>
              <SearchIcon size={16} /> 最近搜索
            </h3>
            <button
              type="button"
              className="parse-iconbtn"
              onClick={onClearRecent}
              aria-label="清空"
            >
              <Trash2 size={14} />
            </button>
          </header>
          <div className="parse-chips">
            {recent.map((q) => (
              <button
                key={q}
                type="button"
                className="parse-chip"
                onClick={() => onPickRecent(q)}
              >
                {q}
              </button>
            ))}
          </div>
        </section>
      ) : null}
      {history.length > 0 ? (
        <section className="parse-panel">
          <header>
            <h3>
              <HistoryIcon size={16} /> 解析历史
            </h3>
            <button
              type="button"
              className="parse-iconbtn"
              onClick={onClearHistory}
              aria-label="清空"
            >
              <Trash2 size={14} />
            </button>
          </header>
          <ul className="parse-history">
            {history.map((h) => (
              <li key={`${h.sourceId}-${h.url}-${h.ts}`}>
                <button
                  type="button"
                  onClick={() => onPickHistory(h)}
                  title={`${h.sourceName}${h.isIframe ? "（iframe）" : ""}`}
                >
                  {h.thumbnail ? (
                    <img src={h.thumbnail} alt="" />
                  ) : (
                    <div className="parse-history__placeholder">
                      <Play size={14} />
                    </div>
                  )}
                  <div className="parse-history__body">
                    <span className="parse-history__title">{h.title}</span>
                    <span className="parse-history__url">
                      {h.sourceName}
                      {h.isIframe ? "（iframe）" : ""}
                    </span>
                  </div>
                </button>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  );
}

// ---------- 持久化 ----------

function readRecent(): string[] {
  try {
    const raw = window.localStorage.getItem(RECENT_KEY);
    if (!raw) return [];
    return JSON.parse(raw) as string[];
  } catch {
    return [];
  }
}

function writeRecent(items: string[]) {
  try {
    window.localStorage.setItem(RECENT_KEY, JSON.stringify(items));
  } catch {
    // ignore
  }
}

function pushRecent(q: string) {
  const cur = readRecent().filter((x) => x !== q);
  cur.unshift(q);
  writeRecent(cur.slice(0, MAX_RECENT));
}

function readHistory(): HistoryEntry[] {
  try {
    const raw = window.localStorage.getItem(HISTORY_KEY);
    if (!raw) return [];
    return JSON.parse(raw) as HistoryEntry[];
  } catch {
    return [];
  }
}

function writeHistory(items: HistoryEntry[]) {
  try {
    window.localStorage.setItem(HISTORY_KEY, JSON.stringify(items));
  } catch {
    // ignore
  }
}

function pushHistory(entry: HistoryEntry) {
  const cur = readHistory().filter(
    (x) => !(x.url === entry.url && x.sourceId === entry.sourceId)
  );
  cur.unshift(entry);
  writeHistory(cur.slice(0, MAX_HISTORY));
}
