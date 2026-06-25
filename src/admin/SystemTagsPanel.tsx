import { useEffect, useMemo, useState } from "react";
import { Pencil, Plus, Save, X } from "lucide-react";
import * as api from "./api";
import { useToast } from "./ToastContext";
import { ConfirmModal } from "./ConfirmModal";

type SystemTag = api.SystemTag;

type Draft = {
  label: string;
  aliases: string;
};

function toDraft(t: SystemTag): Draft {
  return { label: t.label, aliases: (t.aliases ?? []).join(", ") };
}

function fromDraft(d: Draft): SystemTag {
  return {
    label: d.label.trim(),
    aliases: d.aliases
      .split(/[,，、\s]+/)
      .map((x) => x.trim())
      .filter(Boolean),
  };
}

/**
 * 后台编辑 system 标签集合：label + 别名。整体替换保存（PUT /tags/system）。
 */
export function SystemTagsPanel() {
  const [open, setOpen] = useState(false);
  const [tags, setTags] = useState<SystemTag[]>([]);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [confirmReset, setConfirmReset] = useState(false);
  const { show } = useToast();

  async function refresh() {
    setLoading(true);
    try {
      const data = await api.getSystemTags();
      setTags(data);
      setDrafts(Object.fromEntries(data.map((t) => [t.label, toDraft(t)])));
    } catch (e) {
      show(e instanceof Error ? e.message : "加载系统标签失败", "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (open && tags.length === 0 && !loading) {
      refresh();
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  function update(label: string, patch: Partial<Draft>) {
    setDrafts((prev) => ({ ...prev, [label]: { ...(prev[label] ?? { label, aliases: "" }), ...patch } }));
  }

  function addRow() {
    const key = `__new_${Date.now()}`;
    setDrafts((prev) => ({ ...prev, [key]: { label: "", aliases: "" } }));
  }

  function removeRow(label: string) {
    setDrafts((prev) => {
      const next = { ...prev };
      delete next[label];
      return next;
    });
  }

  const dirty = useMemo(() => {
    return Object.entries(drafts).some(([key, draft]) => {
      if (key.startsWith("__new_")) return true;
      const orig = tags.find((t) => t.label === key);
      if (!orig) return true;
      const normalized = fromDraft(draft);
      return (
        normalized.label !== orig.label ||
        normalized.aliases.join("\n") !== (orig.aliases ?? []).join("\n")
      );
    });
  }, [drafts, tags]);

  async function save() {
    const list = Object.values(drafts)
      .map(fromDraft)
      .filter((t) => t.label !== "");
    const seen = new Set<string>();
    for (const t of list) {
      if (seen.has(t.label)) {
        show(`标签「${t.label}」重复`, "error");
        return;
      }
      seen.add(t.label);
    }
    if (list.length === 0) {
      show("至少保留一个标签", "error");
      return;
    }
    setSaving(true);
    try {
      await api.putSystemTags(list);
      show(`已保存 ${list.length} 个系统标签`, "success");
      await refresh();
    } catch (e) {
      show(e instanceof Error ? e.message : "保存失败", "error");
    } finally {
      setSaving(false);
    }
  }

  function reset() {
    setConfirmReset(false);
    setDrafts(Object.fromEntries(tags.map((t) => [t.label, toDraft(t)])));
  }

  return (
    <div className="admin-card system-tags-panel">
      <button
        type="button"
        className="system-tags-panel__toggle"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        <Pencil size={14} />
        <span>System 标签（分类管理）</span>
        <span className="system-tags-panel__count">{tags.length}</span>
      </button>
      {open && (
        <div className="system-tags-panel__body">
          <p className="system-tags-panel__hint">
            System 标签用于文件名自动归类和首页分类导航。修改后会立即影响新扫描的视频，历史视频保留原有标签。
          </p>

          {loading ? (
            <div className="admin-loading-state">加载中...</div>
          ) : (
            <div className="system-tags-panel__rows">
              {Object.entries(drafts).map(([key, draft]) => {
                const isNew = key.startsWith("__new_");
                return (
                  <div className="system-tags-row" key={key}>
                    <input
                      type="text"
                      className="system-tags-row__label"
                      value={draft.label}
                      onChange={(e) => update(key, { label: e.target.value })}
                      placeholder="标签名"
                    />
                    <input
                      type="text"
                      className="system-tags-row__aliases"
                      value={draft.aliases}
                      onChange={(e) => update(key, { aliases: e.target.value })}
                      placeholder="别名（逗号分隔）"
                    />
                    <button
                      type="button"
                      className="admin-btn is-danger"
                      onClick={() => removeRow(key)}
                      aria-label={isNew ? "取消新增" : "删除该标签"}
                      title={isNew ? "取消新增" : "删除该标签"}
                    >
                      <X size={13} />
                    </button>
                  </div>
                );
              })}
            </div>
          )}

          <div className="system-tags-panel__actions">
            <button type="button" className="admin-btn" onClick={addRow}>
              <Plus size={13} /> 新增标签
            </button>
            <button
              type="button"
              className="admin-btn"
              onClick={reset}
              disabled={!dirty || saving}
            >
              撤销改动
            </button>
            <button
              type="button"
              className="admin-btn is-primary"
              onClick={save}
              disabled={!dirty || saving}
            >
              <Save size={13} /> {saving ? "保存中..." : "保存"}
            </button>
          </div>

          <ConfirmModal
            open={confirmReset}
            title="撤销所有改动？"
            message="当前未保存的修改会被丢弃。"
            confirmText="撤销"
            onCancel={() => setConfirmReset(false)}
            onConfirm={reset}
          />
        </div>
      )}
    </div>
  );
}