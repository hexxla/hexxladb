# PackedCoord v1 bit layout (design gate)

This document locks the 128-bit `PackedCoord` encoding used for Morton-ordered keys. If the layout changes incompatibly, bump engine format version / magic per [DEVELOPMENT_ROADMAP.md](../../docs/hexxladb/DEVELOPMENT_ROADMAP.md).

## Coordinate bounds

- Axial `Coord` values accepted by `Pack` satisfy **|Q| ≤ MaxAxialAbs** and **|R| ≤ MaxAxialAbs** with `MaxAxialAbs = 2^18 − 1`, and the derived cube coordinate **S = −Q − R** satisfies **|S| ≤ MaxAxialAbs** (see `bounds.go`).
- **Overflow:** out-of-range coordinates return **`ErrCoordOutOfRange`** — no saturation or clamping.

## Zigzag

Per axis, signed cube coordinates are mapped with 64-bit zigzag:

`zigzag(n) = (n << 1) ^ (n >> 63)` on `int64`, producing an unsigned limb. Valid packed coordinates yield limbs **≤ 2^21 − 1** (21 payload bits per axis).

## Morton interleave

Let `q′`, `r′`, `s′` be the zigzag limbs for cube `(Q, R, S)`.

Bits are interleaved in **fixed cyclic order** `q′₀`, `r′₀`, `s′₀`, `q′₁`, `r′₁`, `s′₁`, … for bit indices `0 … 20`, producing **63 bits** (`21 × 3`). Those bits occupy **`Lo` bits 0–62**; **`Lo` bit 63 is 0** in v1. **`Hi` is 0** in v1 (reserved for a future super-hex / region prefix in the high word).

## 128-bit map

- `PackedCoord[0]` = **`Lo`** (bits 0–63 of the logical 128-bit key).
- `PackedCoord[1]` = **`Hi`** (bits 64–127).

**Total order** (`PackedCoord.Compare`): compare **`Hi` as unsigned**, then **`Lo` as unsigned** (big-endian 128-bit style).

## Serialization note

When writing raw words to disk or wire, document whether each `uint64` is little- or big-endian. Logical **bit 0** is the **LSB of `Lo`**.
