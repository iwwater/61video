import { ReactElement } from "react";
import { AlertTriangle, Loader2, RefreshCw } from "lucide-react";

type Props = {
  /**
   * 加载提示文案。空字符串时不显示文案。
   * 默认"加载中…"。整个区域作为 aria-live=polite，文案会被屏幕阅读器播报。
   */
  label?: string;
  /**
   * 加载样式：
   * - spinner：旋转图标（默认）
   * - skeleton：占位骨架（适合列表/卡片）
   * - inline：只显示图标，无文案，适合按钮内
   */
  variant?: "spinner" | "skeleton" | "inline";
  /**
   * 自定义类名（覆盖默认布局/对齐）。
   */
  className?: string;
  /**
   * 错误文案。设置后组件切换为错误态：显示红色感叹号 + 文案 + 可选重试按钮。
   * 同时把 aria-busy 改为 false，让屏幕阅读器播报错误而不是"加载中"。
   */
  error?: string;
  /**
   * 错误态下的重试回调。提供时显示"重试"按钮，不提供则只显示文案。
   */
  onRetry?: () => void;
  /**
   * 重试按钮文案。默认"重试"。
   */
  retryLabel?: string;
};

/**
 * 统一的页面/区块加载占位组件。
 * 用于替换 Suspense fallback={null}，避免空白闪烁。
 *
 * 使用：
 *   <Suspense fallback={<PageLoading />}>
 *     <LazyPage />
 *   </Suspense>
 */
export function PageLoading({
  label = "加载中…",
  variant = "spinner",
  className,
  error,
  onRetry,
  retryLabel = "重试",
}: Props): ReactElement {
  // 错误态优先：传入 error 字符串就强制走错误样式，忽略 variant。
  if (error) {
    const errorClasses = ["page-loading", "page-loading--error"];
    if (className) errorClasses.push(className);
    return (
      <div
        className={errorClasses.join(" ")}
        role="alert"
        aria-live="assertive"
        aria-busy="false"
      >
        <AlertTriangle
          className="page-loading__error-icon"
          aria-hidden="true"
        />
        <p className="page-loading__error-text">{error}</p>
        {onRetry ? (
          <button
            type="button"
            className="page-loading__retry"
            onClick={onRetry}
          >
            <RefreshCw size={14} aria-hidden="true" />
            <span>{retryLabel}</span>
          </button>
        ) : null}
      </div>
    );
  }

  const classes = ["page-loading"];
  if (variant !== "inline") classes.push(`page-loading--${variant}`);
  if (className) classes.push(className);

  if (variant === "skeleton") {
    return (
      <div className={classes.join(" ")} aria-busy="true" aria-live="polite">
        <div className="page-loading__skeleton-row" />
        <div className="page-loading__skeleton-row page-loading__skeleton-row--short" />
        <div className="page-loading__skeleton-row" />
        {label ? <span className="page-loading__sr-only">{label}</span> : null}
      </div>
    );
  }

  return (
    <div className={classes.join(" ")} aria-busy="true" aria-live="polite">
      <Loader2 className="page-loading__spinner" aria-hidden="true" />
      {label ? <p className="page-loading__label">{label}</p> : null}
    </div>
  );
}

export default PageLoading;
