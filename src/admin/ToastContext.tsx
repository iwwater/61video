import {
  ReactNode,
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import { X } from "lucide-react";

type ToastKind = "info" | "success" | "error";
type Toast = { id: number; kind: ToastKind; text: string };

type Ctx = {
  show: (text: string, kind?: ToastKind) => void;
};

const ToastCtx = createContext<Ctx | null>(null);
const TOAST_DISMISS_MS = 2600;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Toast[]>([]);
  const timers = useRef(new Map<number, ReturnType<typeof window.setTimeout>>());
  const idsByText = useRef(new Map<string, number>());
  const pinnedToastIDs = useRef(new Set<number>());

  const isDismissPaused = useCallback((id: number) => {
    return pinnedToastIDs.current.has(id);
  }, []);

  const clearDismissTimer = useCallback((id: number) => {
    const timer = timers.current.get(id);
    if (!timer) return;
    window.clearTimeout(timer);
    timers.current.delete(id);
  }, []);

  const removeToast = useCallback(
    (id: number, text: string) => {
      clearDismissTimer(id);
      pinnedToastIDs.current.delete(id);
      if (idsByText.current.get(text) === id) {
        idsByText.current.delete(text);
      }
      setItems((list) => list.filter((t) => t.id !== id));
    },
    [clearDismissTimer]
  );

  const scheduleDismiss = useCallback(
    (id: number, text: string) => {
      clearDismissTimer(id);
      if (isDismissPaused(id)) return;
      timers.current.set(
        id,
        window.setTimeout(() => removeToast(id, text), TOAST_DISMISS_MS)
      );
    },
    [clearDismissTimer, isDismissPaused, removeToast]
  );

  const pinDismiss = useCallback(
    (id: number) => {
      pinnedToastIDs.current.add(id);
      clearDismissTimer(id);
    },
    [clearDismissTimer]
  );

  // Deduplicate: same text won't stack, just resets the dismiss timer
  const show = useCallback(
    (text: string, kind: ToastKind = "info") => {
      const existingID = idsByText.current.get(text);
      if (existingID !== undefined) {
        setItems((list) =>
          list.map((t) => (t.id === existingID ? { ...t, kind } : t))
        );
        scheduleDismiss(existingID, text);
        return;
      }
      const id = Date.now() + Math.random();
      idsByText.current.set(text, id);
      setItems((list) => [...list, { id, kind, text }]);
      scheduleDismiss(id, text);
    },
    [scheduleDismiss]
  );

  useEffect(() => {
    return () => {
      for (const timer of timers.current.values()) {
        window.clearTimeout(timer);
      }
      timers.current.clear();
      idsByText.current.clear();
      pinnedToastIDs.current.clear();
    };
  }, []);

  return (
    <ToastCtx.Provider value={{ show }}>
      {children}
      <div className="admin-toast-stack" role="status" aria-live="polite">
        {items.map((t) => (
          <div
            key={t.id}
            className={`admin-toast is-${t.kind}`}
            onClick={() => pinDismiss(t.id)}
          >
            <span className="admin-toast__text">{t.text}</span>
            <button
              type="button"
              className="admin-toast__close"
              aria-label="关闭提示"
              onPointerDown={(event) => event.stopPropagation()}
              onClick={(event) => {
                event.stopPropagation();
                removeToast(t.id, t.text);
              }}
            >
              <X size={16} strokeWidth={2.4} />
            </button>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}

export function useToast(): Ctx {
  const ctx = useContext(ToastCtx);
  if (!ctx) throw new Error("useToast must be used inside <ToastProvider>");
  return ctx;
}

// 小工具：自动关闭的 toast 倒计时，用于某些异步提示展示后返回
export function useFlashError(): [string | null, (msg: string | null) => void] {
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    if (!err) return;
    const t = window.setTimeout(() => setErr(null), 4000);
    return () => window.clearTimeout(t);
  }, [err]);
  return [err, setErr];
}
