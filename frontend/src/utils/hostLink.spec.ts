import { describe, expect, it } from 'vitest'
import {
  buildHref,
  isHostname,
  isIpLiteral,
  normalizeHttpUrl,
  resolveAssetHref,
  resolveSubdomainHref,
  splitHostValues
} from './hostLink'

describe('buildHref', () => {
  it('只认 http(s) 绝对地址，unknown 不会进 href', () => {
    expect(buildHref('https://s3.etoro.com')).toBe('https://s3.etoro.com')
    expect(buildHref('http://a.example.com:8080/x')).toBe('http://a.example.com:8080/x')
    expect(buildHref('unknown://s3.etoro.com')).toBeNull()
    expect(buildHref('')).toBeNull()
    expect(buildHref(null)).toBeNull()
  })

  it('from-data 不猜协议，assume-https 才补 https（https 优先）', () => {
    expect(buildHref('s3.etoro.com')).toBeNull()
    expect(buildHref('s3.etoro.com', 'assume-https')).toBe('https://s3.etoro.com')
    expect(buildHref('162.125.80.6', 'assume-https')).toBe('https://162.125.80.6')
    expect(buildHref('*.example.com', 'assume-https')).toBeNull()
    expect(buildHref('not a host', 'assume-https')).toBeNull()
  })
})

describe('resolveAssetHref', () => {
  it('url 合法就用 url；service=unknown 不给链接', () => {
    expect(resolveAssetHref({ type: 'http', url: 'https://example.com' }, ['example.com'])).toBe(
      'https://example.com'
    )
    expect(resolveAssetHref({ type: 'other', service: 'unknown', url: '' }, ['s3.etoro.com'])).toBeNull()
    expect(resolveAssetHref({ type: 'other', service: 'unknown', url: '' }, ['23.206.24.114'])).toBeNull()
  })

  it('service 明确 http/https 时按 service 拼', () => {
    expect(resolveAssetHref({ type: 'http', service: 'https', url: '' }, ['a.example.com'])).toBe(
      'https://a.example.com'
    )
    expect(resolveAssetHref({ type: 'other', service: 'http', url: '' }, ['a.example.com'])).toBe(
      'http://a.example.com'
    )
  })
})

describe('resolveSubdomainHref', () => {
  it('只有 A/AAAA/CNAME 给链接，TXT/NS 不猜', () => {
    expect(resolveSubdomainHref({ type: 'A' }, 'dnevnik.api.getdelta.io')).toBe(
      'https://dnevnik.api.getdelta.io'
    )
    expect(resolveSubdomainHref({ type: 'CNAME' }, 'x.example.com')).toBe('https://x.example.com')
    expect(resolveSubdomainHref({ type: 'TXT' }, 'x.example.com')).toBeNull()
    expect(resolveSubdomainHref({ type: 'NS' }, 'x.example.com')).toBeNull()
  })
})

describe('splitHostValues', () => {
  it('数组 / 换行 / 逗号 / 空格都能拆成单值', () => {
    expect(splitHostValues(['a.com', 'b.com'])).toEqual(['a.com', 'b.com'])
    expect(splitHostValues('a.com\nb.com')).toEqual(['a.com', 'b.com'])
    expect(splitHostValues('a.com, b.com;c.com')).toEqual(['a.com', 'b.com', 'c.com'])
    expect(splitHostValues(null)).toEqual([])
  })

  it('拆得开无分隔拼接的脏值（历史数据 host 被粘成一串）', () => {
    const dirty =
      'comwww.ig.comnew.nasdaq.cometherscan.iowww.rakuten-sec.co.jpjfinfo.comwww.etoro.com'
    expect(splitHostValues(dirty)).toEqual([
      'www.ig.com',
      'new.nasdaq.com',
      'etherscan.io',
      'www.rakuten-sec.co.jp',
      'jfinfo.com',
      'www.etoro.com'
    ])
  })

  it('正常单值不被误拆', () => {
    expect(splitHostValues('lecturiastrologice.api.getdelta.io')).toEqual([
      'lecturiastrologice.api.getdelta.io'
    ])
    expect(splitHostValues('https://ask.getdelta.io/coin-requests?category=50x')).toEqual([
      'https://ask.getdelta.io/coin-requests?category=50x'
    ])
  })
})

describe('isHostname / isIpLiteral / normalizeHttpUrl', () => {
  it('主机名判定', () => {
    expect(isHostname('s3.etoro.com')).toBe(true)
    expect(isHostname('com')).toBe(false)
    expect(isHostname('*.example.com')).toBe(false)
    expect(isHostname('23.206.24.114')).toBe(false)
  })

  it('IP 判定', () => {
    expect(isIpLiteral('23.206.24.114')).toBe(true)
    expect(isIpLiteral('999.1.1.1')).toBe(false)
  })

  it('残缺 url 归一成 null', () => {
    expect(normalizeHttpUrl('http://')).toBeNull()
    expect(normalizeHttpUrl('example.com')).toBeNull()
    expect(normalizeHttpUrl('http://example.com')).toBe('http://example.com')
  })
})
