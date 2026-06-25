import { useEffect, useMemo, useState } from "react";
import {
  Database,
  Eye,
  EyeOff,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Search as SearchIcon,
  Trash2,
  X,
} from "lucide-react";
import * as api from "./api";
import { useToast } from "./ToastContext";
import { ConfirmModal } from "./ConfirmModal";

type FormState = {
  id: string;
  name: string;
  apiUrl: string;
  playUrlMode: api.ResourceSite["playUrlMode"];
  enabled: boolean;
  sort: number;
  note: string;
};

const EMPTY: FormState = {
  id: "",
  name: "",
  apiUrl: "",
  playUrlMode: "first",
  enabled: true,
  sort: 0,
  note: "",
};

// 常用资源站预设。URL 形如 https://<host>/api.php/provide/vod/?ac=list&wd={kw}，
// 期望返回标准 JSON：{ "code": 1, "list": [{ "vod_id": ..., "vod_name": ...,
// "vod_pic": ..., "vod_remarks": ..., "vod_play_url": "第1集$url#..." }] }
const PRESETS: Array<{
  name: string;
  apiUrl: string;
  note: string;
}> = [
  {
    name: "示例：通用资源站（请替换为你验证可用的真实站点）",
    apiUrl: "https://example.com/api.php/provide/vod/?ac=list&wd={kw}",
    note: "请到网上搜索「资源站 API 列表」找一个稳定的真实站点替换。",
  },
];

export function ResourceSitesPage() {
  const [items, setItems] = useState<api.ResourceSite[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<FormState | null>(null);
  const [saving, setSaving] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [kw, setKw] = useState("");
  const { show } = useToast();

  async function refresh() {
    setLoading(true);
    try {
      const r = await api.listResourceSites();
      setItems(r.items);
    } catch (e) {
      show(e instanceof Error ? e.message : "加载失败", "error");
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
    if (!editing.apiUrl.includes("{kw}")) {
      show("API URL 必须包含 {kw} 占位符", "error");
      return;
    }
    setSaving(true);
    try {
      await api.upsertResourceSite({
        id: editing.id.trim(),
        name: editing.name.trim(),
        apiUrl: editing.apiUrl.trim(),
        playUrlMode: editing.playUrlMode,
        enabled: editing.enabled,
        sort: editing.sort,
        note: editing.note,
      });
      show("已保存", "success");
      setEditing(null);
      await refresh();
    } catch (e) {
      show(e instanceof Error ? e.message : "保存失败", "error");
    } finally {
      setSaving(false);
    }
  }

  async function handleToggle(s: api.ResourceSite) {
    try {
      await api.upsertResourceSite({ ...s, enabled: !s.enabled });
      await refresh();
    } catch (e) {
      show(e instanceof Error ? e.message : "切换失败", "error");
    }
  }

  async function handleDelete(id: string) {
    try {
      await api.deleteResourceSite(id);
      show("已删除", "success");
      await refresh();
    } catch (e) {
      show(e instanceof Error ? e.message : "删除失败", "error");
    } finally {
      setDeletingId(null);
    }
  }

  function startEdit(s?: api.ResourceSite) {
    if (s) {
      setEditing({
        id: s.id,
        name: s.name,
        apiUrl: s.apiUrl,
        playUrlMode: s.playUrlMode,
        enabled: s.enabled,
        sort: s.sort,
        note: s.note,
      });
    } else {
      setEditing({ ...EMPTY, id: `rs-${Date.now()}` });
    }
  }

  return (
    <div className="admin-page">
      <div className="ps-header">
        <div className="ps-header__title">
          <Database size={22} />
          <div>
            <h2>影视资源站</h2>
            <p>
              行业标准 JSON API：<code>?ac=list&amp;wd={"{kw}"}</code>，返回{" "}
              <code>{`{ code, list: [...] }`}</code>。
              前台 <code>/anime</code> 搜索会并发拉每个启用源的结果。
            </p>
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
            <Plus size={16} /> 新增资源站
          </button>
        </div>
      </div>

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

      {loading ? (
        <div className="ps-loading">加载中…</div>
      ) : items.length === 0 ? (
        <EmptyState onAdd={() => startEdit()} />
      ) : filtered.length === 0 ? (
        <div className="ps-empty-filter">没有匹配「{kw}」的资源站</div>
      ) : (
        <div className="ps-grid">
          {filtered.map((s) => (
            <SiteCard
              key={s.id}
              site={s}
              onEdit={() => startEdit(s)}
              onDelete={() => setDeletingId(s.id)}
              onToggle={() => handleToggle(s)}
            />
          ))}
        </div>
      )}

      {editing ? (
        <EditDrawer
          state={editing}
          onChange={setEditing}
          onClose={() => setEditing(null)}
          onSave={handleSave}
          saving={saving}
        />
      ) : null}

      <ConfirmModal
        open={deletingId !== null}
        title="删除资源站"
        message={`确认删除 "${deletingId}"？此操作不可撤销。`}
        confirmText="删除"
        onConfirm={() => deletingId && handleDelete(deletingId)}
        onCancel={() => setDeletingId(null)}
        danger
      />
    </div>
  );
}

function SiteCard({
  site,
  onEdit,
  onDelete,
  onToggle,
}: {
  site: api.ResourceSite;
  onEdit: () => void;
  onDelete: () => void;
  onToggle: () => void;
}) {
  return (
    <article className={`ps-card ${site.enabled ? "" : "is-disabled"}`}>
      <div className="ps-card__head">
        <div className="ps-card__title">
          <Database size={18} />
          <h3>{site.name}</h3>
        </div>
        <div className="ps-card__head-right">
          <span className={`ps-status ${site.enabled ? "is-on" : "is-off"}`}>
            <span className="ps-status__dot" />
            {site.enabled ? "已启用" : "已禁用"}
          </span>
        </div>
      </div>
      <div className="ps-card__meta">
        <code className="ps-card__id">{site.id}</code>
        <span className="ps-kind ps-kind--parse">
          {site.playUrlMode === "first" ? "首段" : site.playUrlMode === "direct" ? "直链" : "详情页"}
        </span>
      </div>
      {site.note ? <p className="ps-card__note">{site.note}</p> : null}
      <div className="ps-card__urls">
        <div className="ps-card__url">
          <span className="ps-card__url-label">API</span>
          <code>{site.apiUrl}</code>
        </div>
      </div>
      <footer className="ps-card__actions">
        <button
          type="button"
          className="button button--ghost button--sm"
          onClick={onToggle}
        >
          {site.enabled ? <EyeOff size={14} /> : <Eye size={14} />}
          {site.enabled ? "禁用" : "启用"}
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

function EditDrawer({
  state,
  onChange,
  onClose,
  onSave,
  saving,
}: {
  state: FormState;
  onChange: (s: FormState) => void;
  onClose: () => void;
  onSave: () => void;
  saving: boolean;
}) {
  return (
    <>
      <div className="ps-drawer__backdrop" onClick={onClose} />
      <aside className="ps-drawer" role="dialog">
        <header className="ps-drawer__head">
          <h3>{state.name ? `编辑：${state.name}` : "新增资源站"}</h3>
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
          <label className="ps-field">
            <span>ID（唯一标识）</span>
            <input
              type="text"
              value={state.id}
              onChange={(e) => onChange({ ...state, id: e.target.value })}
              placeholder="例如：my-resource"
            />
          </label>

          <label className="ps-field">
            <span>名称（给用户看的）</span>
            <input
              type="text"
              value={state.name}
              onChange={(e) => onChange({ ...state, name: e.target.value })}
              placeholder="例如：示例资源站"
            />
          </label>

          <div className="ps-field-row">
            <label className="ps-field">
              <span>播放 URL 模式</span>
              <select
                value={state.playUrlMode}
                onChange={(e) =>
                  onChange({
                    ...state,
                    playUrlMode: e.target.value as FormState["playUrlMode"],
                  })
                }
              >
                <option value="first">首段（自动识别 m3u8 直链 / 详情页）</option>
                <option value="direct">整体直链</option>
                <option value="detail">整体作为详情页（走 parse 流程）</option>
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

          <label className="ps-field">
            <span>
              API URL 模板 <em>（必含 <code>{"{kw}"}</code>）</em>
            </span>
            <input
              type="text"
              value={state.apiUrl}
              onChange={(e) => onChange({ ...state, apiUrl: e.target.value })}
              placeholder="https://example.com/api.php/provide/vod/?ac=list&wd={kw}"
            />
          </label>

          <label className="ps-field">
            <span>备注</span>
            <input
              type="text"
              value={state.note}
              onChange={(e) => onChange({ ...state, note: e.target.value })}
              placeholder="可填官方说明、稳定性提示等"
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

          <div className="ps-presets">
            <h4>快速套用预设</h4>
            <div className="ps-presets__list">
              {PRESETS.map((p) => (
                <button
                  key={p.name}
                  type="button"
                  className="ps-preset"
                  onClick={() =>
                    onChange({
                      ...state,
                      apiUrl: p.apiUrl,
                      note: p.note,
                    })
                  }
                >
                  <strong>{p.name}</strong>
                  <span>{p.note}</span>
                </button>
              ))}
            </div>
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

function EmptyState({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="ps-empty">
      <div className="ps-empty__icon">
        <Database size={48} />
      </div>
      <h3>还没有配置任何资源站</h3>
      <p>
        添加后前台 <code>/anime</code> 搜剧名会并发拉每个启用源的结果。
        <br />
        占位符：<code>{"{kw}"}</code> 关键词。资源站需支持标准 JSON 协议。
      </p>
      <button type="button" className="button button--primary" onClick={onAdd}>
        <Plus size={16} /> 添加第一个资源站
      </button>
    </div>
  );
}
