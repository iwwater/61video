import { Component, ErrorInfo, ReactElement, ReactNode } from "react";

type Props = {
  children: ReactNode;
  /**
   * 自定义降级 UI。默认显示简洁的「页面出错了」面板。
   * 收到后由父组件完全控制渲染。
   */
  fallback?: ReactNode;
  /**
   * 错误日志回调。默认 console.error。
   * 注入到全局错误上报系统时替换。
   */
  onError?: (error: Error, info: ErrorInfo) => void;
};

type State = {
  hasError: boolean;
  error: Error | null;
};

/**
 * React 错误边界：捕获子组件树在 render / commit / 生命周期里的错误，
 * 显示降级 UI 而非整页白屏。
 *
 * - 必须用 class component（React 18 之前没有 hook 替代）
 * - 不会捕获：异步代码、事件处理器、SSR 边界、自身抛错
 * - 业务上挂到 main.tsx 的 <App /> 外层即可覆盖全站
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false, error: null };

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    if (this.props.onError) {
      this.props.onError(error, info);
      return;
    }
    // 默认输出到控制台，方便排查。后续可换成全局错误上报。
    // eslint-disable-next-line no-console
    console.error("[ErrorBoundary] caught:", error, info.componentStack);
  }

  private handleReset = (): void => {
    this.setState({ hasError: false, error: null });
  };

  private handleReload = (): void => {
    window.location.reload();
  };

  render(): ReactNode {
    if (this.state.hasError) {
      if (this.props.fallback !== undefined) {
        return this.props.fallback;
      }
      return <DefaultErrorPanel error={this.state.error} onReset={this.handleReset} onReload={this.handleReload} />;
    }
    return this.props.children;
  }
}

function DefaultErrorPanel({
  error,
  onReset,
  onReload,
}: {
  error: Error | null;
  onReset: () => void;
  onReload: () => void;
}): ReactElement {
  return (
    <div className="error-boundary">
      <div className="error-boundary__panel" role="alert">
        <h1 className="error-boundary__title">页面出错了</h1>
        <p className="error-boundary__message">
          {error?.message || "未知错误，请稍后重试。"}
        </p>
        {error?.stack ? (
          <pre className="error-boundary__stack">{error.stack}</pre>
        ) : null}
        <div className="error-boundary__actions">
          <button type="button" className="error-boundary__btn" onClick={onReset}>
            重试
          </button>
          <button type="button" className="error-boundary__btn error-boundary__btn--primary" onClick={onReload}>
            刷新页面
          </button>
        </div>
      </div>
    </div>
  );
}

export default ErrorBoundary;
