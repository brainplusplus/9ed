# Terminal & Chat Polish Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refine terminal and chat UI into a cleaner Cursor/Windsurf-style hybrid while preserving existing behavior.

**Architecture:** Keep backend and app behavior intact. Apply targeted React markup improvements in terminal/chat components, add explicit clear-terminal actions, and consolidate most polish into `frontend/src/styles/ide.css` so the new visual system stays centralized.

**Tech Stack:** React 18, TypeScript, Zustand, xterm.js, CSS, WebSocket terminal sessions

---

## Task 1: Terminal header structure and clear actions

**Files:**
- Modify: `frontend/src/components/terminal/TerminalPanel.tsx`
- Modify: `frontend/src/components/TerminalView.tsx`
- Modify: `frontend/src/types.ts`

**Step 1: Add UI-only terminal control state**

Create state in `TerminalPanel.tsx` for:
- active clear action menu open/closed
- per-tab terminal action sequence counter or token
- last terminal action payload for active tab

Actions to support:
- `clear-view`
- `send-clear-command`

**Step 2: Extend terminal view props minimally**

Pass down a prop that lets `TerminalView` react to terminal actions for the active tab only.

Expected shape:

```ts
type TerminalAction = {
  targetTabId: string;
  kind: 'clear-view' | 'send-clear-command';
  nonce: number;
};
```

**Step 3: Implement clear behavior in `TerminalView.tsx`**

- For `clear-view`, call xterm clear/reset methods without closing the session
- For `send-clear-command`, write shell-appropriate input over the existing websocket
- Use shell profile / command / label to choose `cls\r` for PowerShell/cmd and `clear\r` for Unix-like shells

**Step 4: Add terminal header actions**

Add explicit icon + label controls in `TerminalPanel.tsx`:
- primary button: `Clear View`
- secondary menu item: `Send Clear Command`

Keep selector and new-terminal action intact.

**Step 5: Verify terminal behavior manually**

Manual checks:
- active tab can clear visible output without reconnecting
- active tab can receive `clear` / `cls`
- no effect on inactive tabs
- close tab behavior still works

---

## Task 2: Terminal session chip cleanup

**Files:**
- Modify: `frontend/src/components/TerminalTabs.tsx`
- Modify: `frontend/src/styles/ide.css`

**Step 1: Refine tab chip markup**

Adjust `TerminalTabs.tsx` markup so each chip has:
- terminal glyph area
- main label row
- muted status row
- dedicated compact close button

**Step 2: Preserve semantics**

Keep:
- `role="tablist"`
- `role="tab"`
- `aria-selected`
- close button accessibility labels

**Step 3: Add status styling hooks**

Add status-specific classes for:
- `ready`
- `connecting`
- `disconnected`
- `error`

**Step 4: Style chips in `ide.css`**

Create a compact polished chip system:
- smaller height than current oversized capsule
- clearer active state
- subtle hover transitions
- better alignment and spacing

---

## Task 3: Chat panel header and composer polish

**Files:**
- Modify: `frontend/src/components/chat/ChatPanel.tsx`
- Modify: `frontend/src/components/chat/ChatInput.tsx`
- Modify: `frontend/src/components/chat/AgentPicker.tsx`
- Modify: `frontend/src/components/chat/ChatSessionList.tsx`
- Modify: `frontend/src/styles/ide.css`

**Step 1: Clean chat header structure**

Split header into stable regions:
- agent/session identity block
- session switcher / new chat actions

**Step 2: Refine composer layout**

Restructure `ChatInput.tsx` into:
- outer composer shell
- attachment chips region
- textarea region
- actions rail (attach / send / stop)

**Step 3: Remove inline layout styles where practical**

Move positional and visual inline styles into CSS classes where they are static or stylistic.

**Step 4: Keep mention and slash command behavior unchanged**

Do not alter keyboard handling or mention logic; only improve structure/styling hooks.

**Step 5: Style header/composer in `ide.css`**

Apply Cursor/Windsurf-like polish:
- subtle glassy/dense surfaces
- tighter spacing rhythm
- slightly elevated composer shell
- cleaner buttons and dropdown triggers

---

## Task 4: Chat message cards, queue, and permission polish

**Files:**
- Modify: `frontend/src/components/chat/ChatMessage.tsx`
- Modify: `frontend/src/components/chat/ChatQueue.tsx`
- Modify: `frontend/src/components/chat/PermissionDialog.tsx`
- Modify: `frontend/src/styles/ide.css`

**Step 1: Replace noisy emoji-based chrome where practical**

Convert obvious UI chrome icons to lighter-weight glyph/SVG treatment in:
- tool call headers
- permission header
- queue actions

**Step 2: Improve message card hierarchy**

Make these visually distinct but cohesive:
- assistant/user bubbles
- tool cards
- plan blocks
- diff blocks
- context blocks
- thinking blocks

**Step 3: Polish queue visuals**

Refine queued message rows into compact actionable cards with better spacing and hover states.

**Step 4: Polish permission dialog**

Give the permission block stronger hierarchy and more product-grade action buttons while preserving exact approval/reject behavior.

---

## Task 5: Responsive verification and cleanup

**Files:**
- Modify: `frontend/src/styles/ide.css`
- Verify: `frontend/src/apps/ide/IDEWorkspace.tsx`

**Step 1: Verify desktop/tablet/mobile behavior**

Ensure the new terminal/chat controls still fit:
- desktop side-by-side layout
- tablet overlays
- mobile stacked view

**Step 2: Run verification**

Run:

```bash
npx tsc --noEmit
npx vite build
```

Expected:
- TypeScript passes
- Vite build succeeds

**Step 3: Final QA checklist**

- clear actions visible and understandable
- terminal chips no longer look oversized / messy
- chat composer/header feel more premium
- tool/plan/permission cards remain readable
- no core behavior regressions
