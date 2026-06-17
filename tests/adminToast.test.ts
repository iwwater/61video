import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const toastSource = readFileSync(
  new URL("../src/admin/ToastContext.tsx", import.meta.url),
  "utf8"
);
const adminCss = readFileSync(
  new URL("../src/styles/admin.css", import.meta.url),
  "utf8"
);

function ruleBody(css: string, selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = css.match(new RegExp(`${escapedSelector}\\s*\\{([^}]*)\\}`));
  assert.ok(match, `Expected CSS rule for ${selector}`);
  return match[1];
}

function mobileCss(): string {
  const marker = "@media (max-width: 768px)";
  const start = adminCss.indexOf(marker);
  assert.notEqual(start, -1, "Expected mobile admin media query");
  return adminCss.slice(start);
}

test("admin toasts auto-dismiss unless the toast body is clicked", () => {
  assert.match(toastSource, /const TOAST_DISMISS_MS = 2600/);
  assert.match(toastSource, /pinnedToastIDs\.current\.has\(id\)/);
  assert.match(toastSource, /if \(isDismissPaused\(id\)\) return/);
  assert.match(toastSource, /onClick=\{\(\) => pinDismiss\(t\.id\)\}/);
  assert.match(toastSource, /className="admin-toast__close"/);
  assert.match(toastSource, /aria-label="关闭提示"/);
  assert.match(toastSource, /<X size=\{16\} strokeWidth=\{2\.4\} \/>/);
  assert.match(toastSource, /event\.stopPropagation\(\)/);
  assert.match(toastSource, /removeToast\(t\.id, t\.text\)/);
  assert.doesNotMatch(toastSource, /onPointerEnter/);
  assert.doesNotMatch(toastSource, /onPointerLeave/);
});

test("admin toasts fit long messages on mobile", () => {
  const baseToast = ruleBody(adminCss, ".admin-toast");
  const baseText = ruleBody(adminCss, ".admin-toast__text");
  const closeButton = ruleBody(adminCss, ".admin-toast__close");
  const mobileToast = ruleBody(mobileCss(), ".admin-toast");
  const mobileText = ruleBody(mobileCss(), ".admin-toast__text");
  const mobileCloseButton = ruleBody(mobileCss(), ".admin-toast__close");

  assert.match(baseToast, /max-width\s*:\s*min\(520px,\s*calc\(100vw - 48px\)\)/);
  assert.match(baseToast, /padding\s*:\s*14px\s+54px\s+14px\s+18px/);
  assert.match(baseToast, /position\s*:\s*relative/);
  assert.match(baseToast, /overflow-wrap\s*:\s*anywhere/);
  assert.match(baseToast, /touch-action\s*:\s*manipulation/);
  assert.match(baseText, /padding-right\s*:\s*2px/);
  assert.match(closeButton, /position\s*:\s*absolute/);
  assert.match(closeButton, /top\s*:\s*10px/);
  assert.match(closeButton, /right\s*:\s*10px/);
  assert.match(closeButton, /width\s*:\s*30px/);
  assert.match(closeButton, /border\s*:\s*1px\s+solid\s+currentColor/);
  assert.match(closeButton, /cursor\s*:\s*pointer/);
  assert.match(mobileToast, /max-width\s*:\s*100%/);
  assert.match(mobileToast, /max-height\s*:\s*min\(42vh,\s*220px\)/);
  assert.match(mobileToast, /text-align\s*:\s*left/);
  assert.match(mobileText, /max-height\s*:\s*min\(32vh,\s*168px\)/);
  assert.match(mobileText, /overflow-y\s*:\s*auto/);
  assert.match(mobileCloseButton, /width\s*:\s*34px/);
  assert.match(mobileCloseButton, /height\s*:\s*34px/);
});
