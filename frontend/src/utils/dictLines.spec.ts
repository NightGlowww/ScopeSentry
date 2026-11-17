import { describe, expect, it } from 'vitest'
import { splitDictLines } from './dictLines'

describe('splitDictLines', () => {
  it('splits on every line ending the backend accepts', () => {
    expect(splitDictLines('a\nb\r\nc\rd')).toEqual(['a', 'b', 'c', 'd'])
  })

  it('drops blank lines and trims surrounding whitespace', () => {
    expect(splitDictLines('  admin  \n\n\t\nroot\n')).toEqual(['admin', 'root'])
  })

  it('keeps the first occurrence of a duplicated line', () => {
    expect(splitDictLines('admin\nroot\n admin \nroot')).toEqual(['admin', 'root'])
  })

  it('returns an empty list for empty input', () => {
    expect(splitDictLines('')).toEqual([])
    expect(splitDictLines(' \n\r\n ')).toEqual([])
  })

  it('keeps comment-like and url lines untouched', () => {
    expect(splitDictLines('# comment\n/admin\n?x=1')).toEqual(['# comment', '/admin', '?x=1'])
  })
})
