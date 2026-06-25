import { ReactElement } from "react";
import { Loader2 } from "lucide-react";

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
}: Props): ReactElement {
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
