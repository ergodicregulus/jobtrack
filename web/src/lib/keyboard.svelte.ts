/**
 * Keyboard navigation for the feed: j/k to move, Enter to open, s to save.
 *
 * This audience expects it and it is part of what makes the product feel like an
 * instrument rather than a page. It is also strictly an enhancement — every one
 * of these actions is reachable by Tab and a click, so nothing is lost with
 * JavaScript off.
 *
 * Three rules, all learnt from tools that get this wrong:
 *
 *  1. **Never steal a keystroke meant for a field.** A bare `s` while someone is
 *     typing in the search box must type an `s`. Modifier combinations are the
 *     browser's and the OS's, never ours.
 *  2. **Move focus, do not merely highlight.** A separate "selected row" concept
 *     drifts from the focus ring and leaves screen-reader users following a
 *     cursor they cannot perceive. Real focus keeps both in step for free.
 *  3. **Scroll the focused row clear of the sticky header** — WCAG 2.4.11. The
 *     CSS `scroll-margin-top` handles this, so `focus()` is enough.
 */

/** Fields where a bare letter is text the user is typing, not a command. */
function isTypingTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  const tag = el.tagName.toLowerCase();
  return (
    tag === 'input' ||
    tag === 'textarea' ||
    tag === 'select' ||
    el.isContentEditable
  );
}

export interface FeedKeysOptions {
  /** Selector for the focusable link inside each row. */
  rowLink?: string;
  /** Selector for the save control inside each row. */
  saveButton?: string;
}

/**
 * Attaches feed shortcuts for the lifetime of the calling component.
 *
 * Call inside `$effect` so it is removed on unmount — a listener that outlives
 * its page starts acting on the next one.
 */
export function attachFeedKeys(opts: FeedKeysOptions = {}): () => void {
  const rowLink = opts.rowLink ?? 'li.card .title a';
  const saveButton = opts.saveButton ?? 'button.save';

  function rows(): HTMLElement[] {
    return Array.from(document.querySelectorAll<HTMLElement>(rowLink));
  }

  /** Index of the row containing focus, or -1 when focus is elsewhere. */
  function currentIndex(all: HTMLElement[]): number {
    const active = document.activeElement;
    if (!active) return -1;
    const card = active.closest('li.card');
    if (!card) return -1;
    return all.findIndex((link) => link.closest('li.card') === card);
  }

  function move(delta: number) {
    const all = rows();
    if (all.length === 0) return;

    const i = currentIndex(all);
    // From outside the list, j enters at the top and k at the bottom, which is
    // what every list-with-vim-keys does and what people reach for without
    // being told.
    const next = i === -1 ? (delta > 0 ? 0 : all.length - 1) : i + delta;
    const clamped = Math.max(0, Math.min(all.length - 1, next));
    all[clamped]?.focus();
  }

  function saveFocusedRow() {
    const active = document.activeElement;
    const card = active instanceof HTMLElement ? active.closest('li.card') : null;
    // Only the row that already has focus. Acting on a row the user cannot see
    // is worse than doing nothing.
    card?.querySelector<HTMLButtonElement>(saveButton)?.click();
  }

  function onKeydown(e: KeyboardEvent) {
    if (isTypingTarget(e.target)) return;
    // Modifier combinations belong to the browser and the OS.
    if (e.metaKey || e.ctrlKey || e.altKey) return;

    switch (e.key) {
      case 'j':
        e.preventDefault();
        move(1);
        break;
      case 'k':
        e.preventDefault();
        move(-1);
        break;
      case 's': {
        // Enter is deliberately absent: focus is on a real link, so the browser
        // already opens it. Re-implementing that would only be a chance to get
        // middle-click and modifier-click wrong.
        const active = document.activeElement;
        if (active instanceof HTMLElement && active.closest('li.card')) {
          e.preventDefault();
          saveFocusedRow();
        }
        break;
      }
    }
  }

  window.addEventListener('keydown', onKeydown);
  return () => window.removeEventListener('keydown', onKeydown);
}
