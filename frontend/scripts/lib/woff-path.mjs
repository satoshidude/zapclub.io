import { readFileSync } from 'node:fs'
import { inflateSync } from 'node:zlib'

const align4 = (value) => (value + 3) & ~3
const f2dot14 = (buffer, offset) => buffer.readInt16BE(offset) / 16384
const fmt = (value) => Number(value.toFixed(2))

function woffToSfnt(input) {
  if (input.toString('ascii', 0, 4) !== 'wOFF') throw new Error('Expected a WOFF 1 font')

  const flavor = input.readUInt32BE(4)
  const numTables = input.readUInt16BE(12)
  const output = Buffer.alloc(input.readUInt32BE(16))
  const maxPower = 2 ** Math.floor(Math.log2(numTables))

  output.writeUInt32BE(flavor, 0)
  output.writeUInt16BE(numTables, 4)
  output.writeUInt16BE(maxPower * 16, 6)
  output.writeUInt16BE(Math.log2(maxPower), 8)
  output.writeUInt16BE(numTables * 16 - maxPower * 16, 10)

  let outputOffset = 12 + numTables * 16
  for (let index = 0; index < numTables; index++) {
    const sourceRecord = 44 + index * 20
    const sourceOffset = input.readUInt32BE(sourceRecord + 4)
    const compressedLength = input.readUInt32BE(sourceRecord + 8)
    const originalLength = input.readUInt32BE(sourceRecord + 12)
    const compressed = input.subarray(sourceOffset, sourceOffset + compressedLength)
    const table = compressedLength < originalLength ? inflateSync(compressed) : compressed
    if (table.length !== originalLength) throw new Error('Invalid WOFF table length')

    const outputRecord = 12 + index * 16
    input.copy(output, outputRecord, sourceRecord, sourceRecord + 4)
    output.writeUInt32BE(input.readUInt32BE(sourceRecord + 16), outputRecord + 4)
    output.writeUInt32BE(outputOffset, outputRecord + 8)
    output.writeUInt32BE(originalLength, outputRecord + 12)
    table.copy(output, outputOffset)
    outputOffset += align4(originalLength)
  }

  return output
}

function fontTables(buffer) {
  const tables = new Map()
  const numTables = buffer.readUInt16BE(4)
  for (let index = 0; index < numTables; index++) {
    const record = 12 + index * 16
    tables.set(buffer.toString('ascii', record, record + 4), {
      offset: buffer.readUInt32BE(record + 8),
      length: buffer.readUInt32BE(record + 12),
    })
  }
  return tables
}

function cmapLookup(buffer, cmap) {
  const subtables = []
  const count = buffer.readUInt16BE(cmap.offset + 2)
  for (let index = 0; index < count; index++) {
    const record = cmap.offset + 4 + index * 8
    const platform = buffer.readUInt16BE(record)
    const encoding = buffer.readUInt16BE(record + 2)
    const offset = cmap.offset + buffer.readUInt32BE(record + 4)
    const format = buffer.readUInt16BE(offset)
    if (format === 4 || format === 12) subtables.push({ platform, encoding, offset, format })
  }

  const selected = subtables.find(({ format, platform }) => format === 12 && platform === 3)
    ?? subtables.find(({ format, platform }) => format === 4 && platform === 3)
    ?? subtables[0]
  if (!selected) throw new Error('Font has no supported Unicode cmap')

  if (selected.format === 12) {
    const groups = buffer.readUInt32BE(selected.offset + 12)
    return (codePoint) => {
      for (let index = 0; index < groups; index++) {
        const group = selected.offset + 16 + index * 12
        const start = buffer.readUInt32BE(group)
        const end = buffer.readUInt32BE(group + 4)
        if (codePoint >= start && codePoint <= end) {
          return buffer.readUInt32BE(group + 8) + codePoint - start
        }
      }
      return 0
    }
  }

  const segCount = buffer.readUInt16BE(selected.offset + 6) / 2
  const endCodes = selected.offset + 14
  const startCodes = endCodes + segCount * 2 + 2
  const deltas = startCodes + segCount * 2
  const ranges = deltas + segCount * 2
  return (codePoint) => {
    for (let index = 0; index < segCount; index++) {
      const end = buffer.readUInt16BE(endCodes + index * 2)
      if (codePoint > end) continue
      const start = buffer.readUInt16BE(startCodes + index * 2)
      if (codePoint < start) return 0
      const delta = buffer.readInt16BE(deltas + index * 2)
      const rangeOffset = buffer.readUInt16BE(ranges + index * 2)
      if (rangeOffset === 0) return (codePoint + delta) & 0xffff
      const address = ranges + index * 2 + rangeOffset + (codePoint - start) * 2
      const glyph = buffer.readUInt16BE(address)
      return glyph === 0 ? 0 : (glyph + delta) & 0xffff
    }
    return 0
  }
}

function legacyKerning(buffer, kern) {
  const pairs = new Map()
  if (!kern || buffer.readUInt16BE(kern.offset) !== 0) return pairs

  const count = buffer.readUInt16BE(kern.offset + 2)
  let offset = kern.offset + 4
  for (let index = 0; index < count; index++) {
    const length = buffer.readUInt16BE(offset + 2)
    const coverage = buffer.readUInt16BE(offset + 4)
    if ((coverage >> 8) === 0 && (coverage & 1) === 1) {
      const pairCount = buffer.readUInt16BE(offset + 6)
      for (let pairIndex = 0; pairIndex < pairCount; pairIndex++) {
        const pair = offset + 14 + pairIndex * 6
        const left = buffer.readUInt16BE(pair)
        const right = buffer.readUInt16BE(pair + 2)
        pairs.set(`${left}:${right}`, buffer.readInt16BE(pair + 4))
      }
    }
    offset += length
  }
  return pairs
}

function simpleContours(buffer, offset, contourCount) {
  const endpoints = []
  for (let index = 0; index < contourCount; index++) endpoints.push(buffer.readUInt16BE(offset + 10 + index * 2))
  const pointCount = endpoints.at(-1) + 1
  let cursor = offset + 10 + contourCount * 2
  cursor += 2 + buffer.readUInt16BE(cursor)

  const flags = []
  while (flags.length < pointCount) {
    const flag = buffer[cursor++]
    flags.push(flag)
    if (flag & 8) {
      const repeats = buffer[cursor++]
      for (let index = 0; index < repeats; index++) flags.push(flag)
    }
  }

  const xs = []
  let x = 0
  for (const flag of flags) {
    if (flag & 2) x += (flag & 16 ? 1 : -1) * buffer[cursor++]
    else if (!(flag & 16)) {
      x += buffer.readInt16BE(cursor)
      cursor += 2
    }
    xs.push(x)
  }

  const ys = []
  let y = 0
  for (const flag of flags) {
    if (flag & 4) y += (flag & 32 ? 1 : -1) * buffer[cursor++]
    else if (!(flag & 32)) {
      y += buffer.readInt16BE(cursor)
      cursor += 2
    }
    ys.push(y)
  }

  const contours = []
  let start = 0
  for (const end of endpoints) {
    const contour = []
    for (let index = start; index <= end; index++) {
      contour.push({ x: xs[index], y: ys[index], on: Boolean(flags[index] & 1) })
    }
    contours.push(contour)
    start = end + 1
  }
  return contours
}

function transformContours(contours, a, b, c, d, dx, dy) {
  return contours.map((contour) => contour.map((point) => ({
    x: point.x * a + point.y * c + dx,
    y: point.x * b + point.y * d + dy,
    on: point.on,
  })))
}

function contoursToPath(contours, xOffset, baseline, scale) {
  const point = ({ x, y }) => ({ x: fmt(xOffset + x * scale), y: fmt(baseline - y * scale) })
  const midpoint = (a, b) => ({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, on: true })
  const commands = []

  for (const contour of contours) {
    if (contour.length === 0) continue
    const first = contour[0]
    const last = contour.at(-1)
    const start = first.on ? first : last.on ? last : midpoint(last, first)
    const ordered = first.on ? contour.slice(1) : contour
    ordered.push(start)
    const startPoint = point(start)
    commands.push(`M${startPoint.x} ${startPoint.y}`)

    for (let index = 0; index < ordered.length; index++) {
      const current = ordered[index]
      if (current.on) {
        const target = point(current)
        commands.push(`L${target.x} ${target.y}`)
        continue
      }
      const next = ordered[index + 1] ?? start
      const target = point(next.on ? next : midpoint(current, next))
      const control = point(current)
      commands.push(`Q${control.x} ${control.y} ${target.x} ${target.y}`)
      if (next.on) index++
    }
    commands.push('Z')
  }

  return commands.join('')
}

export function loadWoffFont(path) {
  const buffer = woffToSfnt(readFileSync(path))
  const tables = fontTables(buffer)
  const table = (name) => {
    const value = tables.get(name)
    if (!value) throw new Error(`Font table ${name} is missing`)
    return value
  }
  const head = table('head')
  const hhea = table('hhea')
  const hmtx = table('hmtx')
  const loca = table('loca')
  const glyf = table('glyf')
  const maxp = table('maxp')
  const unitsPerEm = buffer.readUInt16BE(head.offset + 18)
  const numGlyphs = buffer.readUInt16BE(maxp.offset + 4)
  const numMetrics = buffer.readUInt16BE(hhea.offset + 34)
  const longLoca = buffer.readInt16BE(head.offset + 50) === 1
  const glyphFor = cmapLookup(buffer, table('cmap'))
  const kerning = legacyKerning(buffer, tables.get('kern'))
  const advances = []
  let lastAdvance = 0
  for (let index = 0; index < numGlyphs; index++) {
    if (index < numMetrics) lastAdvance = buffer.readUInt16BE(hmtx.offset + index * 4)
    advances.push(lastAdvance)
  }

  const glyphOffset = (glyph) => glyf.offset + (longLoca
    ? buffer.readUInt32BE(loca.offset + glyph * 4)
    : buffer.readUInt16BE(loca.offset + glyph * 2) * 2)

  const glyphContours = (glyph, depth = 0) => {
    if (depth > 8) throw new Error('Composite glyph nesting is too deep')
    const offset = glyphOffset(glyph)
    if (offset === glyphOffset(glyph + 1)) return []
    const contourCount = buffer.readInt16BE(offset)
    if (contourCount >= 0) return simpleContours(buffer, offset, contourCount)

    const contours = []
    let cursor = offset + 10
    let flags = 0
    do {
      flags = buffer.readUInt16BE(cursor)
      const componentGlyph = buffer.readUInt16BE(cursor + 2)
      cursor += 4
      const words = Boolean(flags & 1)
      let arg1 = words ? buffer.readInt16BE(cursor) : buffer.readInt8(cursor)
      let arg2 = words ? buffer.readInt16BE(cursor + 2) : buffer.readInt8(cursor + 1)
      cursor += words ? 4 : 2
      if (!(flags & 2)) [arg1, arg2] = [0, 0]

      let a = 1; let b = 0; let c = 0; let d = 1
      if (flags & 8) {
        a = d = f2dot14(buffer, cursor)
        cursor += 2
      } else if (flags & 64) {
        a = f2dot14(buffer, cursor)
        d = f2dot14(buffer, cursor + 2)
        cursor += 4
      } else if (flags & 128) {
        a = f2dot14(buffer, cursor)
        b = f2dot14(buffer, cursor + 2)
        c = f2dot14(buffer, cursor + 4)
        d = f2dot14(buffer, cursor + 6)
        cursor += 8
      }
      contours.push(...transformContours(glyphContours(componentGlyph, depth + 1), a, b, c, d, arg1, arg2))
    } while (flags & 32)
    return contours
  }

  const layout = (text, fontSize, letterSpacing = 0) => {
    const scale = fontSize / unitsPerEm
    const glyphs = [...text].map((character) => glyphFor(character.codePointAt(0)))
    let width = 0
    for (let index = 0; index < glyphs.length; index++) {
      if (index > 0) width += (kerning.get(`${glyphs[index - 1]}:${glyphs[index]}`) ?? 0) * scale + letterSpacing
      width += advances[glyphs[index]] * scale
    }
    return { glyphs, scale, width }
  }

  return {
    width(text, fontSize, letterSpacing = 0) {
      return layout(text, fontSize, letterSpacing).width
    },
    path(text, x, baseline, fontSize, letterSpacing = 0) {
      const { glyphs, scale } = layout(text, fontSize, letterSpacing)
      let cursor = x
      let pathData = ''
      for (let index = 0; index < glyphs.length; index++) {
        const glyph = glyphs[index]
        if (index > 0) cursor += (kerning.get(`${glyphs[index - 1]}:${glyph}`) ?? 0) * scale + letterSpacing
        pathData += contoursToPath(glyphContours(glyph), cursor, baseline, scale)
        cursor += advances[glyph] * scale
      }
      return pathData
    },
  }
}
