# Atlas identity

**Status:** Agreed for now (2026-09-28). The Folded Path direction is the Atlas app icon and logo until a later explicit brand decision changes it.

The three overlapping ribbon planes suggest a thought becoming a clear next step. The mark stays recognizable without the wordmark at iPhone icon size. Use the mark with the `ATLAS` wordmark when space allows, and the mark alone for compact placements.

## Master assets

| Asset | Use |
| --- | --- |
| `atlas-mark.svg` | Transparent standalone symbol |
| `atlas-logo.svg` | Symbol and wordmark on transparent background |
| `atlas-logo-on-dark.svg` | Same lockup with a light wordmark for dark surfaces |
| `atlas-icon.svg` | Full-bleed square app icon, without baked-in rounded corners |

The iPhone asset catalog contains a copy of `atlas-icon.svg` and an opaque 1024 × 1024 RGB PNG exported from it. The testing webpage serves copies of `atlas-mark.svg` for its header and `atlas-icon.svg` for its favicon. Keep these copies visually identical to the master assets when changing the identity.

## Colors

- Icon background and wordmark: `#14172f`
- Upper ribbon: `#ff816e`
- Middle ribbon: `#91a5f8`
- Lower ribbon: `#4d5cb0`
- Fold highlight: `#6578d1`

The SVGs are editable vector masters. The wordmark uses Inter with local sans-serif fallbacks; keep its spacing and weight consistent when exporting another format. Use `atlas-logo.svg` on light surfaces and `atlas-logo-on-dark.svg` on dark surfaces. Let iOS apply the app icon mask, rather than drawing a rounded square into the asset. Do not use the retired mint `A.` icon.
