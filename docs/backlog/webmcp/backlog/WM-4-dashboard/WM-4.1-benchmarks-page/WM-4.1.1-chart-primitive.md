# WM-4.1.1 · Task: Chart primitive

**Status:** ⬜ todo — [#488](https://github.com/petarnenov/ultra-factory/issues/488)

**Type:** frontend. **Estimate:** 0.5 pd. **Story:** WM-4.1.

## Goal

Add the shadcn `chart` primitive at
`frontend/src/renderer/components/ui/chart.tsx` backed by `recharts`, wired
to the existing `--chart-1..5` tokens, so WM-4.1.2 builds its charts from
`components/ui/*`.

## Decisions (resolved)

- Library: **recharts via the shadcn chart component** (not hand-written SVG).

## Context (verified)

- `--chart-1..5` already exist in `frontend/src/styles/tokens.css` for dark
  (lines 43-47) and light (lines 677-681), greyscale oklch. No new tokens.
- Theme selectors: dark is `:root` (default) and `:root.dark`; light is
  `:root[data-theme="light"]` (tokens.css:10-18, 652).
- React `^19.2.7`; recharts 2.x supports React 19 (pin the latest 2.x at
  implementation time and record it in the PR).
- `DESIGN.md` §2: no gradients, glow, or decorative color; color is semantic.
  Series colors map driver → token: `playwright` → `--chart-1`,
  `webmcp-scripted` → `--chart-2`, `webmcp-jev` → `--chart-3`; failed repeats
  use `--destructive` (verify the token name in tokens.css at implementation).

## Blueprint

```tsx
// frontend/src/renderer/components/ui/chart.tsx  (shadcn reference, trimmed)
export type ChartConfig = Record<string, { label: React.ReactNode; color?: string }>;
export function ChartContainer({ config, className, children }: { config: ChartConfig; className?: string; children: React.ReactElement }) {
  // sets --color-<key> from config.color (token var) on the wrapper, renders <ResponsiveContainer>
}
export const ChartTooltip = RechartsPrimitive.Tooltip;
export function ChartTooltipContent(props: ...) { /* shadcn impl, uses bg-popover/text-popover-foreground, no shadow beyond tokens */ }
export const ChartLegend = RechartsPrimitive.Legend;
export function ChartLegendContent(props: ...) { ... }
```

- Only `ChartContainer`, `ChartTooltip(+Content)`, `ChartLegend(+Content)`
  are added; no `ChartStyle` CSS injection with inline `<style>` (CSP meta is
  injected by `injectCspMeta` in `vite.renderer.config.ts`; verify
  `style-src` allows what shadcn emits, else set the variables via the
  wrapper's `style` prop instead).
- Fonts and sizes follow `styles.css` semantic classes; axis text uses
  `text-muted-foreground` at the existing small size.

## Subtasks

- [ ] `npm --prefix frontend i -D recharts@<pinned>`; report bundle delta
  from `npm run build:web` in the PR.
- [ ] `components/ui/chart.tsx` as above.
- [ ] Vitest render test: `ChartContainer` with a `BarChart` renders an
  `svg` and sets `--color-playwright` from config.
- [ ] CSP check: `npm run build:web`, open the page, no `style-src`
  violations in the console (document the result).

## Acceptance criteria

- [ ] A bar chart renders in both themes using only `--chart-*` tokens.
- [ ] `npm run typecheck`, `npm run build`, `npm run build:web`, `npm test`
  pass.
- [ ] No new global CSS; no inline-style CSP violation.

## Verification

- `ao preview` of `/#/benchmarks` (WM-4.1.2) or a temporary dev route in
  both themes; screenshots in the PR.

## Dependencies

None (parallel with WM-2).
