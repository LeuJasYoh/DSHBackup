import { onMounted, onUnmounted, type Ref } from "vue";

const selector = "button:not([disabled]),input:not([disabled]):not([type='hidden']),select:not([disabled]),textarea:not([disabled]),summary,[tabindex]:not([tabindex='-1'])";
interface ModalEntry {
  card: HTMLElement;
  overlay: HTMLElement;
  returnTarget: HTMLElement | null;
  onEscape?: () => void;
}
const stack: ModalEntry[] = [];
const inertBefore = new Map<HTMLElement, boolean>();
function items(card: HTMLElement) {
  return Array.from(card.querySelectorAll<HTMLElement>(selector)).filter((element) =>
    !element.matches(":disabled") && !element.closest("[inert]") && element.getClientRects().length > 0,
  );
}
function focusFirst(entry: ModalEntry) {
  const target = items(entry.card).find((item) => item.hasAttribute("autofocus")) ?? items(entry.card)[0] ?? entry.card;
  target.focus({ preventScroll: true });
}
function updateLayers() {
  inertBefore.forEach((value, element) => { element.inert = value; });
  inertBefore.clear();
  stack.forEach((entry, index) => { entry.overlay.style.zIndex = String(70 + index); });
  const top = stack[stack.length - 1];
  if (!top) return;
  // Inert every sibling along the top dialog's ancestor path, including lower overlays and app chrome.
  let node: HTMLElement = top.overlay;
  while (node.parentElement) {
    for (const sibling of Array.from(node.parentElement.children)) {
      if (sibling === node || !(sibling instanceof HTMLElement)) continue;
      inertBefore.set(sibling, sibling.inert);
      sibling.inert = true;
    }
    node = node.parentElement;
    if (node === document.body) break;
  }
}
function onKeydown(event: KeyboardEvent) {
  const top = stack[stack.length - 1];
  if (!top) return;
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopImmediatePropagation();
    top.onEscape?.();
    return;
  }
  if (event.key !== "Tab") return;
  const targets = items(top.card);
  const first = targets[0] ?? top.card;
  const last = targets[targets.length - 1] ?? top.card;
  const active = document.activeElement;
  if (!targets.length || !top.card.contains(active) || (event.shiftKey ? active === first : active === last)) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
  }
}
function onFocus(event: FocusEvent) {
  const top = stack[stack.length - 1];
  if (top && !top.card.contains(event.target as Node)) focusFirst(top);
}
export function useModalFocus(card: Ref<HTMLElement | null>, onEscape?: () => void) {
  let entry: ModalEntry | undefined;
  onMounted(() => {
    if (!card.value) return;
    entry = {
      card: card.value,
      overlay: card.value.closest<HTMLElement>(".overlay") ?? card.value,
      returnTarget: document.activeElement instanceof HTMLElement ? document.activeElement : null,
      onEscape,
    };
    entry.card.tabIndex = -1;
    if (!stack.length) {
      document.addEventListener("keydown", onKeydown, true);
      document.addEventListener("focusin", onFocus, true);
    }
    stack.push(entry);
    updateLayers();
    focusFirst(entry);
  });
  onUnmounted(() => {
    if (!entry) return;
    const index = stack.indexOf(entry);
    const wasTop = index === stack.length - 1;
    // If a parent closes before its child, carry its return target forward.
    for (const child of stack.slice(index + 1)) {
      if (child.returnTarget && entry.overlay.contains(child.returnTarget)) child.returnTarget = entry.returnTarget;
    }
    if (index >= 0) stack.splice(index, 1);
    updateLayers();
    if (!stack.length) {
      document.removeEventListener("keydown", onKeydown, true);
      document.removeEventListener("focusin", onFocus, true);
    }
    if (!wasTop) return;
    const top = stack[stack.length - 1];
    const target = entry.returnTarget;
    if (target?.isConnected && !target.closest("[inert]") && !target.matches(":disabled") && target.getClientRects().length && (!top || top.card.contains(target))) target.focus({ preventScroll: true });
    else if (top) focusFirst(top);
    else document.querySelector<HTMLElement>(".nav-item.active")?.focus({ preventScroll: true });
  });
}
