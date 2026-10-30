import { mount, type VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import OptionSelect from "./OptionSelect.vue";

const options = [
  { value: 0, label: "自动" },
  { value: 16, label: "16 MB" },
  { value: 32, label: "32 MB" },
];

let wrapper: VueWrapper | null = null;

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
  wrapper?.unmount();
  wrapper = null;
  document.body.innerHTML = "";
});

function render(modelValue: number | string = 0) {
  wrapper = mount(OptionSelect, {
    props: { modelValue, options, ariaLabel: "字典大小" },
    attachTo: document.body,
  });
  return wrapper;
}

const panel = () => document.querySelector<HTMLElement>(".option-panel");
const trigger = (w: VueWrapper) => w.get<HTMLButtonElement>(".option-trigger");

describe("OptionSelect", () => {
  it("starts closed as a combobox", () => {
    const w = render();
    const button = trigger(w);
    expect(button.attributes("role")).toBe("combobox");
    expect(button.attributes("aria-expanded")).toBe("false");
    expect(button.text()).toContain("自动");
    expect(panel()).toBeNull();
  });

  it("opens a listbox with the current value selected", async () => {
    const w = render(16);
    await trigger(w).trigger("click");
    const list = panel();
    expect(list).not.toBeNull();
    expect(list!.getAttribute("role")).toBe("listbox");
    expect(list!.querySelectorAll('[role="option"]')).toHaveLength(3);
    expect(list!.querySelector('[aria-selected="true"]')?.textContent).toContain("16 MB");
    expect(trigger(w).attributes("aria-expanded")).toBe("true");
  });

  it("moves with the arrow keys and commits with Enter", async () => {
    const w = render(0);
    const button = trigger(w);
    await button.trigger("click");
    await button.trigger("keydown", { key: "ArrowDown" });
    await button.trigger("keydown", { key: "ArrowDown" });
    await button.trigger("keydown", { key: "Enter" });
    expect(w.emitted("update:modelValue")?.[0]).toEqual([32]);
    expect(w.emitted("change")?.[0]).toEqual([32]);
    expect(panel()).toBeNull();
    expect(document.activeElement).toBe(button.element);
  });

  it("wraps from the first item up to the last", async () => {
    const w = render(0);
    const button = trigger(w);
    await button.trigger("click");
    await button.trigger("keydown", { key: "ArrowUp" });
    await button.trigger("keydown", { key: "Enter" });
    expect(w.emitted("update:modelValue")?.[0]).toEqual([32]);
  });

  it("closes on Escape without selecting but returns focus to the trigger", async () => {
    const w = render(0);
    const button = trigger(w);
    await button.trigger("click");
    await button.trigger("keydown", { key: "Escape" });
    expect(panel()).toBeNull();
    expect(w.emitted("update:modelValue")).toBeUndefined();
    expect(document.activeElement).toBe(button.element);
  });

  it("supports typeahead", async () => {
    const w = render(0);
    const button = trigger(w);
    await button.trigger("click");
    await button.trigger("keydown", { key: "3" });
    await button.trigger("keydown", { key: "2" });
    await button.trigger("keydown", { key: "Enter" });
    expect(w.emitted("update:modelValue")?.[0]).toEqual([32]);
  });

  it("selects by pointer and closes", async () => {
    const w = render(0);
    await trigger(w).trigger("click");
    const items = panel()!.querySelectorAll<HTMLButtonElement>(".option-item");
    items[1].dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await w.vm.$nextTick();
    expect(w.emitted("update:modelValue")?.[0]).toEqual([16]);
    expect(panel()).toBeNull();
  });

  it("does not open when disabled", async () => {
    const w = mount(OptionSelect, {
      props: { modelValue: 0, options, disabled: true },
      attachTo: document.body,
    });
    wrapper = w;
    await trigger(w).trigger("click");
    expect(panel()).toBeNull();
  });
});
