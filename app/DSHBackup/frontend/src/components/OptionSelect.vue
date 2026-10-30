<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";

export interface SelectOption {
  value: number | string;
  label: string;
  hint?: string;
}

const props = withDefaults(defineProps<{
  modelValue: number | string;
  options: SelectOption[];
  ariaLabel?: string;
  inputId?: string;
  disabled?: boolean;
  autofocus?: boolean;
  placeholder?: string;
}>(), { ariaLabel: "", inputId: "", disabled: false, autofocus: false, placeholder: "请选择" });

const emit = defineEmits<{ "update:modelValue": [number | string]; change: [number | string] }>();

const trigger = ref<HTMLButtonElement | null>(null);
const panel = ref<HTMLElement | null>(null);
const open = ref(false);
const activeIndex = ref(-1);
const placement = ref<"top" | "bottom">("bottom");
const position = ref({ left: 0, top: 0, width: 0, maxHeight: 320 });
const mountTarget = ref<HTMLElement | string>("body");
let typeahead = "";
let typeaheadTimer: ReturnType<typeof setTimeout> | null = null;

const listId = `option-list-${Math.random().toString(36).slice(2, 9)}`;
const optionId = (index: number) => `${listId}-${index}`;
const selectedIndex = computed(() => props.options.findIndex((option) => option.value === props.modelValue));
const selected = computed(() => selectedIndex.value >= 0 ? props.options[selectedIndex.value] : null);
const displayLabel = computed(() => selected.value?.label ?? props.placeholder);
const activeId = computed(() => open.value && activeIndex.value >= 0 ? optionId(activeIndex.value) : undefined);

// The panel is teleported so it is never clipped; inside a modal it must land
// inside the overlay, otherwise the modal's inert pass would disable it.
function openList() {
  if (props.disabled || !props.options.length) return;
  mountTarget.value = trigger.value?.closest<HTMLElement>(".overlay") ?? "body";
  open.value = true;
  activeIndex.value = Math.max(selectedIndex.value, 0);
  void nextTick(() => { measure(); scrollActive(); });
}

function closeList(focus = false) {
  if (!open.value) return;
  open.value = false;
  typeahead = "";
  if (focus) trigger.value?.focus({ preventScroll: true });
}

function measure() {
  const el = trigger.value;
  if (!el) return;
  const rect = el.getBoundingClientRect();
  const spaceBelow = window.innerHeight - rect.bottom - 12;
  const spaceAbove = rect.top - 12;
  const wanted = Math.min(340, Math.max(120, (panel.value?.scrollHeight ?? 0) + 8));
  const flip = spaceBelow < Math.min(wanted, 180) && spaceAbove > spaceBelow;
  placement.value = flip ? "top" : "bottom";
  const maxHeight = Math.max(120, Math.min(340, flip ? spaceAbove : spaceBelow));
  const width = Math.min(rect.width, window.innerWidth - 16);
  position.value = {
    left: Math.max(8, Math.min(rect.left, window.innerWidth - width - 8)),
    top: flip ? Math.max(8, rect.top - Math.min(wanted, maxHeight) - 6) : rect.bottom + 6,
    width,
    maxHeight,
  };
}

function scrollActive() {
  panel.value?.querySelector<HTMLElement>(`[data-index="${activeIndex.value}"]`)?.scrollIntoView({ block: "nearest" });
}

function choose(index: number) {
  const option = props.options[index];
  if (!option) return;
  emit("update:modelValue", option.value);
  emit("change", option.value);
  closeList(true);
}

function move(delta: number) {
  if (!open.value) { openList(); return; }
  const count = props.options.length;
  activeIndex.value = (activeIndex.value + delta + count) % count;
  void nextTick(scrollActive);
}

function typeAhead(char: string) {
  typeahead += char.toLowerCase();
  if (typeaheadTimer) clearTimeout(typeaheadTimer);
  typeaheadTimer = setTimeout(() => { typeahead = ""; }, 600);
  const index = props.options.findIndex((option) => option.label.toLowerCase().startsWith(typeahead));
  if (index < 0) return;
  if (!open.value) openList();
  activeIndex.value = index;
  void nextTick(scrollActive);
}

function onKeydown(event: KeyboardEvent) {
  if (props.disabled) return;
  switch (event.key) {
    case "ArrowDown": event.preventDefault(); move(1); return;
    case "ArrowUp": event.preventDefault(); move(-1); return;
    case "Home":
      if (open.value) { event.preventDefault(); activeIndex.value = 0; void nextTick(scrollActive); }
      return;
    case "End":
      if (open.value) { event.preventDefault(); activeIndex.value = props.options.length - 1; void nextTick(scrollActive); }
      return;
    case "Enter":
      event.preventDefault();
      if (open.value) choose(activeIndex.value); else openList();
      return;
    case " ":
      event.preventDefault();
      if (open.value) choose(activeIndex.value); else openList();
      return;
    case "Escape":
      if (open.value) { event.preventDefault(); closeList(true); }
      return;
    case "Tab":
      closeList();
      return;
  }
  if (event.key.length === 1 && !event.ctrlKey && !event.altKey && !event.metaKey) typeAhead(event.key);
}

// Escape inside an open panel must close the panel, not the surrounding modal:
// the modal listens on document capture, so this has to run on window capture.
function onWindowKeydown(event: KeyboardEvent) {
  if (!open.value || event.key !== "Escape") return;
  event.preventDefault();
  event.stopPropagation();
  closeList(true);
}

function onDocumentPointerDown(event: PointerEvent) {
  if (!open.value) return;
  const target = event.target as Node;
  if (trigger.value?.contains(target) || panel.value?.contains(target)) return;
  closeList();
}

function onViewportChange() { if (open.value) measure(); }

function onScroll(event: Event) {
  if (!open.value) return;
  const target = event.target as Node;
  if (panel.value && (target === panel.value || panel.value.contains(target))) return;
  closeList();
}

function bind() {
  document.addEventListener("pointerdown", onDocumentPointerDown, true);
  window.addEventListener("keydown", onWindowKeydown, true);
  window.addEventListener("resize", onViewportChange);
  window.addEventListener("scroll", onScroll, true);
}
function unbind() {
  document.removeEventListener("pointerdown", onDocumentPointerDown, true);
  window.removeEventListener("keydown", onWindowKeydown, true);
  window.removeEventListener("resize", onViewportChange);
  window.removeEventListener("scroll", onScroll, true);
}

watch(open, (value) => { if (value) bind(); else unbind(); });
watch(() => props.disabled, (value) => { if (value) closeList(); });
watch(() => props.options, () => { if (open.value) void nextTick(measure); });
onBeforeUnmount(() => {
  unbind();
  if (typeaheadTimer) clearTimeout(typeaheadTimer);
});
</script>

<template>
  <div class="option-select">
    <button
      ref="trigger"
      :id="inputId || undefined"
      type="button"
      class="text-input option-trigger"
      role="combobox"
      aria-haspopup="listbox"
      :aria-label="ariaLabel || undefined"
      :aria-expanded="open"
      :aria-controls="open ? listId : undefined"
      :aria-activedescendant="activeId"
      :disabled="disabled"
      :autofocus="autofocus || undefined"
      @click="open ? closeList() : openList()"
      @keydown="onKeydown"
    >
      <span class="option-trigger-label" :class="{ 'is-placeholder': !selected }">{{ displayLabel }}</span>
      <svg class="icon option-chevron" :class="{ 'is-open': open }" aria-hidden="true"><use href="#i-chevron-right"></use></svg>
    </button>
    <Teleport :to="mountTarget">
      <div
        v-if="open"
        ref="panel"
        :id="listId"
        class="option-panel"
        :class="`is-${placement}`"
        role="listbox"
        :aria-label="ariaLabel || undefined"
        :style="{ left: `${position.left}px`, top: `${position.top}px`, width: `${position.width}px`, maxHeight: `${position.maxHeight}px` }"
      >
        <button
          v-for="(option, index) in options"
          :id="optionId(index)"
          :key="String(option.value)"
          type="button"
          role="option"
          class="option-item"
          :class="{ 'is-active': index === activeIndex, 'is-selected': option.value === modelValue }"
          :data-index="index"
          :aria-selected="option.value === modelValue"
          @mousedown.prevent
          @pointermove="activeIndex = index"
          @click="choose(index)"
        >
          <span class="option-item-label">{{ option.label }}</span>
          <span v-if="option.hint" class="option-item-hint">{{ option.hint }}</span>
          <svg v-if="option.value === modelValue" class="icon option-check" aria-hidden="true"><use href="#i-check"></use></svg>
        </button>
        <div v-if="!options.length" class="option-empty">没有可选项目</div>
      </div>
    </Teleport>
  </div>
</template>
