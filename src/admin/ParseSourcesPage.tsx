import { useEffect, useMemo, useState } from "react";
import {
  Activity,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Eye,
  EyeOff,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Search as SearchIcon,
  TestTube2,
  Trash2,
  Tv,
  Wand2,
  X,
  XCircle,
} from "lucide-react";
import * as api from "./api";
import { useToast } from "./ToastContext";
import { ConfirmModal } from "./ConfirmModal";
import { searchAnime } from "@/data/anime";

type FormState = {
  id: string;
  name: string;
  kind: api.ParseSource["kind"];
  searchUrl: string;
  parseUrl: string;
  enabled: boolean;
  sort: number;
  note: string;
};

const EMPTY: FormState = {
  id: "",
  name: "",
  kind: "both",
  searchUrl: "",
  parseUrl: "",
  enabled: true,
  sort: 0,
  note: "",
};

const PRESETS: Array<{
  name: string;
  kind: api.ParseSource["kind"];
  searchUrl?: string;
  parseUrl?: string;
  note: string;
}> = [
  {
    name: "通用 HTML 兜底",
    kind: "parse",
    parseUrl: "https://example.com/play/{url}",
    note: "内置 universal 已支持，本条仅作演示",
  },
  {
    name: "示例：搜索型",
    kind: "search",
    searchUrl: "https://example.com/search?wd={kw}",
    note: "把 {kw} 替换为关键词",
  },
];

export function ParseSourcesPage() {
  const [items, setItems] = useState<api.ParseSource[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<FormState | null>(null);
  const [saving, setSaving] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [kw, setKw] = useState("");
  const [testing, setTesting] = useState<{
    source: api.ParseSource;
    type: "search" | "parse";
    value: string;
  } | null>(null);
  const [testResult, setTestResult] = useState<{
    ok: boolean;
    message: string;
  } | null>(null);
  const [healthRunning, setHealthRunning] = useState(false);
  const { show } = useToast();

  async function refresh() {
    setLoading(true);
    try {
      const r = await api.listParseSources();
      setItems(r.items);
    } catch (e) {
      const msg = e instanceof Error ? e.message : "加载失败";
      show(msg, "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const filtered = useMemo(() => {
    if (!kw.trim()) return items;
    const q = kw.toLowerCase();
    return items.filter(
      (s) =>
        s.name.toLowerCase().includes(q) ||
        s.id.toLowerCase().includes(q) ||
        s.note.toLowerCase().includes(q)
    );
  }, [items, kw]);

  const enabledCount = items.filter((s) => s.enabled).length;
  const totalCount = items.length;

  async function handleSave() {
    if (!editing) return;
    if (!editing.id.trim() || !editing.name.trim()) {
      show("ID 和名称必填", "error");
      return;
    }
    if (
      (editing.kind === "search" || editing.kind === "both") &&
      !editing.searchUrl.includes("{kw}")
    ) {
      show("搜索 URL 必须包含 {kw} 占位符", "error");
      return;
    }
    if (
      (editing.kind === "parse" || editing.kind === "both") &&
      !editing.parseUrl.includes("{url}")
    ) {
      show("解析 URL 必须包含 {url} 占位符", "error");
      return;
    }
    setSaving(true);
    try {
      await api.upsertParseSource({
        id: editing.id.trim(),
        name: editing.name.trim(),
        kind: editing.kind,
        searchUrl: editing.searchUrl.trim(),
        parseUrl: editing.parseUrl.trim(),
        enabled: editing.enabled,
        sort: editing.sort,
        note: editing.note,
      });
      show("已保存", "success");
      setEditing(null);
      await refresh();
    } catch (e) {
      const msg = e instanceof Error ? e.message : "保存失败";
      show(msg, "error");
    } finally {
      setSaving(false);
    }
  }

  async function handleToggle(src: api.ParseSource) {
    try {
      await api.upsertParseSource({ ...src, enabled: !src.enabled });
      await refresh();
    } catch (e) {
      show(e instanceof Error ? e.message : "切换失败", "error");
    }
  }

  async function handleDelete(id: string) {
    try {
      await api.deleteParseSource(id);
      show("已删除", "success");
      await refresh();
    } catch (e) {
      show(e instanceof Error ? e.message : "删除失败", "error");
    } finally {
      setDeletingId(null);
    }
  }

  function startEdit(src?: api.ParseSource) {
    if (src) {
      setEditing({
        id: src.id,
        name: src.name,
        kind: src.kind,
        searchUrl: src.searchUrl,
        parseUrl: src.parseUrl,
        enabled: src.enabled,
        sort: src.sort,
        note: src.note,
      });
    } else {
      setEditing({ ...EMPTY, id: `src-${Date.now()}` });
    }
    setTestResult(null);
  }

  function applyPreset(preset: (typeof PRESETS)[number]) {
    if (!editing) return;
    setEditing({
      ...editing,
      kind: preset.kind,
      searchUrl: preset.searchUrl ?? editing.searchUrl,
      parseUrl: preset.parseUrl ?? editing.parseUrl,
      note: preset.note,
    });
  }

  async function runTest(input: string) {
    if (!testing) return;
    setTestResult(null);
    try {
      if (testing.type === "search") {
        const r = await searchAnime(input);
        setTestResult({
          ok: true,
          message: `搜索「${input}」共 ${r.total} 条（本地 ${r.localCount} · 外站 ${r.remoteCount}）。源「${testing.source.name}」已包含在外站结果中。`,
        });
      } else {
        const res = await fetch("/api/anime/parse", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          credentials: "include",
          body: JSON.stringify({ url: input }),
        });
        if (!res.ok) {
          const t = await res.text();
          setTestResult({ ok: false, message: `HTTP ${res.status}：${t}` });
          return;
        }
        const j = (await res.json()) as {
          title?: string;
          videoUrl?: string;
          source?: string;
        };
        setTestResult({
          ok: !!j.videoUrl,
          message: j.videoUrl
            ? `解析成功：${j.title || "(无标题)"} → ${j.source || "?"}`
            : "解析未拿到视频源（页面可能没 <video>/<source>/<iframe>）",
        });
      }
    } catch (e) {
      setTestResult({ ok: false, message: e instanceof Error ? e.message : String(e) });
    }
  }

  return (
    <div className="admin-page">
      {/* Header */}
      <div className="ps-header">
        <div className="ps-header__title">
          <Tv size={22} />
          <div>
            <h2>影视解析/搜索源</h2>
            <p>前台 <code>/anime</code> 页面会列出已启用的源，按启用顺序尝试。</p>
          </div>
        </div>
        <div className="ps-header__stats">
          <span className="ps-stat">
            <span className="ps-stat__num">{enabledCount}</span>
            <span className="ps-stat__label">已启用</span>
          </span>
          <span className="ps-stat">
            <span className="ps-stat__num">{totalCount}</span>
            <span className="ps-stat__label">总计</span>
          </span>
        </div>
        <div className="ps-header__actions">
          <button
            type="button"
            className="button button--ghost"
            onClick={async () => {
              setHealthRunning(true);
              try {
                await api.runHealthCheck();
                show("健康检查已运行", "success");
                await refresh();
              } catch (e) {
                show(e instanceof Error ? e.message : "检查失败", "error");
              } finally {
                setHealthRunning(false);
              }
            }}
            disabled={healthRunning}
            title="立即 ping 所有启用的源"
          >
            {healthRunning ? (
              <Activity size={16} className="spin" />
            ) : (
              <TestTube2 size={16} />
            )}
            {healthRunning ? "检测中…" : "立即检测"}
          </button>
          <button
            type="button"
            className="button button--ghost"
            onClick={refresh}
            disabled={loading}
          >
            <RefreshCw size={16} /> 刷新
          </button>
          <button
            type="button"
            className="button button--primary"
            onClick={() => startEdit()}
          >
            <Plus size={16} /> 新增源
          </button>
        </div>
      </div>

      {/* 搜索框 */}
      {items.length > 0 ? (
        <div className="ps-search">
          <SearchIcon size={16} />
          <input
            type="search"
            placeholder="按名称 / ID / 备注过滤"
            value={kw}
            onChange={(e) => setKw(e.target.value)}
          />
          {kw ? (
            <button type="button" onClick={() => setKw("")} aria-label="清空">
              <X size={14} />
            </button>
          ) : null}
        </div>
      ) : null}

      {/* 卡片网格 */}
      {loading ? (
        <div className="ps-loading">加载中…</div>
      ) : items.length === 0 ? (
        <EmptyState onAdd={() => startEdit()} />
      ) : filtered.length === 0 ? (
        <div className="ps-empty-filter">没有匹配「{kw}」的源</div>
      ) : (
        <div className="ps-grid">
          {filtered.map((s) => (
            <SourceCard
              key={s.id}
              source={s}
              onEdit={() => startEdit(s)}
              onDelete={() => setDeletingId(s.id)}
              onToggle={() => handleToggle(s)}
            />
          ))}
        </div>
      )}

      {/* 侧滑抽屉：编辑 */}
      {editing ? (
        <EditDrawer
          state={editing}
          onChange={setEditing}
          onClose={() => setEditing(null)}
          onSave={handleSave}
          onPreset={applyPreset}
          onTest={(type, value) => {
            setTesting({
              source: {
                ...emptySource,
                id: editing.id,
                name: editing.name,
                kind: editing.kind,
              },
              type,
              value,
            });
            setTestResult(null);
          }}
          saving={saving}
          testResult={testResult}
          runTest={runTest}
        />
      ) : null}

      <ConfirmModal
        open={deletingId !== null}
        title="删除解析源"
        message={`确认删除 "${deletingId}"？此操作不可撤销。`}
        confirmText="删除"
        onConfirm={() => deletingId && handleDelete(deletingId)}
        onCancel={() => setDeletingId(null)}
        danger
      />
    </div>
  );
}

// ---------- 卡片 ----------

function HealthBadge({ source }: { source: api.ParseSource }) {
  const status = source.lastHealthStatus;
  if (!status) {
    return (
      <span className="ps-health ps-health--unknown" title="尚未检测">
        <span className="ps-health__dot" />
        未检测
      </span>
    );
  }
  if (status === "ok") {
    return (
      <span
        className="ps-health ps-health--ok"
        title={source.lastHealthError || `可用 · ${source.lastHealthResponseMs ?? 0}ms`}
      >
        <CheckCircle2 size={12} />
        {source.lastHealthResponseMs ? `${source.lastHealthResponseMs}ms` : "可用"}
      </span>
    );
  }
  return (
    <span
      className="ps-health ps-health--fail"
      title={source.lastHealthError || "失败"}
    >
      <XCircle size={12} />
      失效
    </span>
  );
}

function healthAge(ts?: number) {
  if (!ts) return "—";
  const ageMs = Date.now() - ts;
  if (ageMs < 60_000) return "刚刚";
  if (ageMs < 3_600_000) return `${Math.floor(ageMs / 60_000)} 分钟前`;
  if (ageMs < 86_400_000) return `${Math.floor(ageMs / 3_600_000)} 小时前`;
  return `${Math.floor(ageMs / 86_400_000)} 天前`;
}

function SourceCard({
  source,
  onEdit,
  onDelete,
  onToggle,
}: {
  source: api.ParseSource;
  onEdit: () => void;
  onDelete: () => void;
  onToggle: () => void;
}) {
  return (
    <article className={`ps-card ${source.enabled ? "" : "is-disabled"}`}>
      <div className="ps-card__head">
        <div className="ps-card__title">
          <Tv size={18} />
          <h3>{source.name}</h3>
        </div>
        <div className="ps-card__head-right">
          <HealthBadge source={source} />
          <span className={`ps-status ${source.enabled ? "is-on" : "is-off"}`}>
            <span className="ps-status__dot" />
            {source.enabled ? "已启用" : "已禁用"}
          </span>
        </div>
      </div>

      <div className="ps-card__meta">
        <code className="ps-card__id">{source.id}</code>
        <span className={`ps-kind ps-kind--${source.kind}`}>{kindLabel(source.kind)}</span>
      </div>

      {source.note ? <p className="ps-card__note">{source.note}</p> : null}

      {/* 健康检查状态 */}
      {source.lastHealthStatus ? (
        <div
          className={`ps-card__health ${
            source.lastHealthStatus === "ok" ? "is-ok" : "is-fail"
          }`}
        >
          <div className="ps-card__health-row">
            <span className="ps-card__health-label">最近检测：</span>
            <span className="ps-card__health-time">
              {healthAge(source.lastHealthAt)}
            </span>
            {source.lastHealthError ? (
              <span className="ps-card__health-error">
                {source.lastHealthError}
              </span>
            ) : null}
          </div>
        </div>
      ) : null}

      <div className="ps-card__urls">
        {source.searchUrl ? (
          <div className="ps-card__url">
            <span className="ps-card__url-label">搜</span>
            <code>{source.searchUrl}</code>
          </div>
        ) : null}
        {source.parseUrl ? (
          <div className="ps-card__url">
            <span className="ps-card__url-label">解</span>
            <code>{source.parseUrl}</code>
          </div>
        ) : null}
      </div>

      <footer className="ps-card__actions">
        <button
          type="button"
          className="button button--ghost button--sm"
          onClick={onToggle}
        >
          {source.enabled ? <EyeOff size={14} /> : <Eye size={14} />}
          {source.enabled ? "禁用" : "启用"}
        </button>
        <span style={{ flex: 1 }} />
        <button
          type="button"
          className="button button--ghost button--sm"
          onClick={onEdit}
        >
          <Pencil size={14} /> 编辑
        </button>
        <button
          type="button"
          className="button button--danger button--sm"
          onClick={onDelete}
        >
          <Trash2 size={14} />
        </button>
      </footer>
    </article>
  );
}

function kindLabel(kind: api.ParseSource["kind"]) {
  return kind === "search" ? "搜索" : kind === "parse" ? "解析" : "搜索+解析";
}

// ---------- 侧滑抽屉 ----------

function EditDrawer({
  state,
  onChange,
  onClose,
  onSave,
  onPreset,
  onTest,
  saving,
  testResult,
  runTest,
}: {
  state: FormState;
  onChange: (s: FormState) => void;
  onClose: () => void;
  onSave: () => void;
  onPreset: (p: (typeof PRESETS)[number]) => void;
  onTest: (type: "search" | "parse", value: string) => void;
  saving: boolean;
  testResult: { ok: boolean; message: string } | null;
  runTest: (input: string) => void;
}) {
  const [testInput, setTestInput] = useState("");
  const [showPresets, setShowPresets] = useState(false);
  const [testType, setTestType] = useState<"search" | "parse">("search");

  const canSearch = state.kind === "search" || state.kind === "both";
  const canParse = state.kind === "parse" || state.kind === "both";

  return (
    <>
      <div className="ps-drawer__backdrop" onClick={onClose} />
      <aside className="ps-drawer" role="dialog">
        <header className="ps-drawer__head">
          <h3>{state.name ? `编辑：${state.name}` : "新增解析源"}</h3>
          <button
            type="button"
            className="button button--ghost button--sm"
            onClick={onClose}
            aria-label="关闭"
          >
            <X size={16} />
          </button>
        </header>

        <div className="ps-drawer__body">
          {/* 预设 */}
          <div className="ps-presets">
            <button
              type="button"
              className="ps-presets__toggle"
              onClick={() => setShowPresets((v) => !v)}
            >
              <Wand2 size={14} /> 快速套用预设
              {showPresets ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
            </button>
            {showPresets ? (
              <div className="ps-presets__list">
                {PRESETS.map((p) => (
                  <button
                    key={p.name}
                    type="button"
                    className="ps-preset"
                    onClick={() => onPreset(p)}
                  >
                    <strong>{p.name}</strong>
                    <span>{p.note}</span>
                  </button>
                ))}
              </div>
            ) : null}
          </div>

          <label className="ps-field">
            <span>ID（唯一标识）</span>
            <input
              type="text"
              value={state.id}
              onChange={(e) => onChange({ ...state, id: e.target.value })}
              placeholder="例如：sigujx"
            />
          </label>

          <label className="ps-field">
            <span>名称（给用户看的）</span>
            <input
              type="text"
              value={state.name}
              onChange={(e) => onChange({ ...state, name: e.target.value })}
              placeholder="例如：思古解析"
            />
          </label>

          <div className="ps-field-row">
            <label className="ps-field">
              <span>类型</span>
              <select
                value={state.kind}
                onChange={(e) =>
                  onChange({ ...state, kind: e.target.value as FormState["kind"] })
                }
              >
                <option value="both">搜索 + 解析</option>
                <option value="search">仅搜索</option>
                <option value="parse">仅解析</option>
              </select>
            </label>
            <label className="ps-field">
              <span>排序</span>
              <input
                type="number"
                value={state.sort}
                onChange={(e) =>
                  onChange({ ...state, sort: Number(e.target.value) || 0 })
                }
              />
            </label>
          </div>

          {canSearch ? (
            <label className="ps-field">
              <span>
                搜索 URL 模板 <em>（必含 <code>{"{kw}"}</code>）</em>
              </span>
              <input
                type="text"
                value={state.searchUrl}
                onChange={(e) => onChange({ ...state, searchUrl: e.target.value })}
                placeholder="https://example.com/search?wd={kw}"
              />
            </label>
          ) : null}

          {canParse ? (
            <label className="ps-field">
              <span>
                解析 URL 模板 <em>（必含 <code>{"{url}"}</code>）</em>
              </span>
              <input
                type="text"
                value={state.parseUrl}
                onChange={(e) => onChange({ ...state, parseUrl: e.target.value })}
                placeholder="https://example.com/parse?url={url}"
              />
            </label>
          ) : null}

          <label className="ps-field">
            <span>备注</span>
            <input
              type="text"
              value={state.note}
              onChange={(e) => onChange({ ...state, note: e.target.value })}
              placeholder="可填官方说明、限制等"
            />
          </label>

          <label className="ps-field-row ps-field--inline">
            <input
              type="checkbox"
              checked={state.enabled}
              onChange={(e) => onChange({ ...state, enabled: e.target.checked })}
            />
            <span>启用</span>
          </label>

          {/* 测试 */}
          <div className="ps-test">
            <h4>
              <TestTube2 size={16} /> 测试（保存前即可试）
            </h4>
            <div className="ps-test__tabs">
              {canSearch ? (
                <button
                  type="button"
                  className={`ps-test__tab ${testType === "search" ? "is-active" : ""}`}
                  onClick={() => setTestType("search")}
                >
                  测搜索
                </button>
              ) : null}
              {canParse ? (
                <button
                  type="button"
                  className={`ps-test__tab ${testType === "parse" ? "is-active" : ""}`}
                  onClick={() => setTestType("parse")}
                >
                  测解析
                </button>
              ) : null}
            </div>
            <div className="ps-test__form">
              <input
                type="text"
                value={testInput}
                onChange={(e) => setTestInput(e.target.value)}
                placeholder={
                  testType === "search" ? "测试关键词" : "测试视频页面 URL"
                }
              />
              <button
                type="button"
                className="button button--ghost button--sm"
                disabled={!testInput.trim()}
                onClick={() => {
                  onTest(testType, testInput);
                  runTest(testInput);
                }}
              >
                跑
              </button>
            </div>
            {testResult ? (
              <div
                className={`ps-test__result ${
                  testResult.ok ? "is-ok" : "is-fail"
                }`}
              >
                {testResult.message}
              </div>
            ) : null}
          </div>
        </div>

        <footer className="ps-drawer__foot">
          <button type="button" className="button button--ghost" onClick={onClose}>
            取消
          </button>
          <button
            type="button"
            className="button button--primary"
            onClick={onSave}
            disabled={saving}
          >
            <Save size={14} /> {saving ? "保存中…" : "保存"}
          </button>
        </footer>
      </aside>
    </>
  );
}

// ---------- 空状态 ----------

function EmptyState({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="ps-empty">
      <div className="ps-empty__icon">
        <Tv size={48} />
      </div>
      <h3>还没有配置任何解析源</h3>
      <p>
        添加后前台 <code>/anime</code> 页面会列出它们，可按关键词搜剧名或解析视频页面。
        <br />
        占位符：<code>{"{kw}"}</code> 关键词，<code>{"{url}"}</code> 源视频链接。
      </p>
      <button type="button" className="button button--primary" onClick={onAdd}>
        <Plus size={16} /> 添加第一个源
      </button>
    </div>
  );
}

// ---------- 工具 ----------

const emptySource: api.ParseSource = {
  id: "",
  name: "",
  kind: "both",
  searchUrl: "",
  parseUrl: "",
  enabled: false,
  sort: 0,
  note: "",
  createdAt: 0,
  updatedAt: 0,
};
