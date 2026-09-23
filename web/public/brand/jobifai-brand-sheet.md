# Jobifai visual identity

## Brand idea
Jobifai is a calm automation partner for job applications across professions. The mark preserves the product's existing metaphor: anchor/role target -> search journey/pipeline -> check/application confidence.

## Primary tagline
**Your calm application partner.**

Alternatives:
- Find roles. Apply with confidence.
- Less application admin. More career focus.
- Automated applications, with you in control.

## Logo construction
- Anchor circle: a role or target discovered.
- Curved path: search, evaluation and application pipeline.
- Terminal check: a suitable action completed with confidence.
- Rounded 2.2-unit strokes keep the mark human and operational rather than technical or aggressive.
- Wordmark: Inter Semibold, sentence case `Jobifai`, tracking `-0.025em`.

### Clear space
Use the anchor diameter (**x**) as the minimum clear space on every side of the standalone mark and lockup.

### Minimum size
- Standalone mark: **16 px** digital minimum.
- Horizontal lockup: **96 px** minimum width digital; use mark-only below this.
- Print mark: **5 mm** minimum height.

## Colors
| Token | Hex | OKLCH approximation |
|---|---|---|
| Primary accent | `#7B6CF5` | `oklch(0.616 0.198 283.9)` |
| Accent highlight | `#8B7CF8` | `oklch(0.658 0.178 285.9)` |
| Light-theme accent | `#5B4FD9` | `oklch(0.522 0.203 280.6)` |
| Dark background | `#171821` | `oklch(0.212 0.018 279.8)` |
| Dark surface | `#22232F` | `oklch(0.261 0.022 281.0)` |
| Light background | `#F8F9FC` | `oklch(0.982 0.004 271.4)` |
| Dark text | `#F4F4F7` | `oklch(0.968 0.004 286.3)` |
| Light text | `#20212A` | `oklch(0.251 0.017 280.1)` |

### Usage
- Dark UI: mark `#7B6CF5`, check `#8B7CF8`, on `#171821` or similarly dark neutral surfaces.
- Light UI: use `#5B4FD9` for the complete mark when stronger contrast is needed.
- Monochrome: one solid ink/color only; never mix success/warning colors into the consumer logo.
- Admin amber is UI-only and never part of the Jobifai consumer mark.

## Incorrect usage
Do not:
1. rotate, skew or stretch the mark;
2. redraw the check as a separate green success icon;
3. add rainbow/neon gradients;
4. put `JobifAI`, `JOBIFAI`, or `Jobif AI` in the lockup;
5. use robot, brain, code-terminal or briefcase symbols as substitutes;
6. place the colored mark on backgrounds with insufficient contrast;
7. add shadows/glows to the core logo;
8. reduce the lockup below 96 px wide - use the mark instead.

## Maskable icons
All important mark geometry sits within the center **80%** safe square. The supplied app icon places the mark at about 62% of canvas width, leaving generous mask tolerance.

## Manifest colors
```json
{
  "theme_color": "#171821",
  "background_color": "#171821"
}
```
For light-theme browser chrome, switch the runtime `<meta name="theme-color">` to `#F8F9FC`.

## Export notes
- PNGs: sRGB, flat vector-derived artwork.
- PWA icons: full-bleed `#7B6CF5` background with white mark; no text.
- `jobifai-mark-transparent-512.png`: transparent mark-only export for presentations/marketing layouts.
- Wordmark appears only in lockups, OG images, banner and splash placeholders.
