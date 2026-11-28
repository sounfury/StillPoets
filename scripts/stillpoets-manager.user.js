// ==UserScript==
// @name         StillPoets 书库管理与 Z-Library 投递助手
// @namespace    https://github.com/sounfury/StillPoets
// @version      0.1.3
// @description  StillPoets 零部署 WebDAV 诗集分组管理、EPUB 批量投递与 Z-Library 一键导入助手
// @author       sounfury
// @license      MIT
// @homepageURL  https://github.com/sounfury/StillPoets
// @supportURL   https://github.com/sounfury/StillPoets/issues
// @downloadURL  https://raw.githubusercontent.com/sounfury/StillPoets/main/scripts/stillpoets-manager.user.js
// @updateURL    https://raw.githubusercontent.com/sounfury/StillPoets/main/scripts/stillpoets-manager.user.js
// @match        *://*/*
// @grant        GM_xmlhttpRequest
// @grant        GM_getValue
// @grant        GM_setValue
// @grant        GM_deleteValue
// @grant        GM_registerMenuCommand
// @grant        GM_openInTab
// @connect      *
// @run-at       document-idle
// ==/UserScript==

(function () {
  'use strict';

  // ============================================================================
  // 1. 协议常量与纯逻辑（与 Go internal/sync/protocol.go & merger.go 严格对齐）
  // ============================================================================

  const PROTOCOL_SCHEMA_VERSION = 2;
  const INBOX_SCHEMA_VERSION = 1;
  const INBOX_PARSER_RULE_VERSION = 1;
  const STATE_DIR = 'state';
  const BOOKS_DIR = 'books';
  const INBOX_DIR = 'inbox';
  const INBOX_READY_SUFFIX = '.ready.json';
  const KEEP_SNAPSHOT_GENERATIONS = 2;

  const MAX_SNAPSHOT_COMPRESSED_BYTES = 16 * 1024 * 1024;
  const MAX_SNAPSHOT_RAW_BYTES = 64 * 1024 * 1024;
  const MAX_INBOX_READY_BYTES = 64 * 1024;
  const MAX_INBOX_EPUB_BYTES = 64 * 1024 * 1024;

  const MANAGER_ENTRY_URL = 'https://github.com/sounfury/StillPoets#stillpoets-manager';
  const MANAGER_HASH = '#stillpoets-manager';

  const STORAGE_KEYS = {
    config: 'sp_webdav_config_v1',
    installId: 'sp_install_id_v1',
    localRev: 'sp_local_rev_v1',
    localCounter: 'sp_local_counter_v1',
    zlibHosts: 'sp_zlib_hosts_v1',
    cachedGroups: 'sp_cached_groups_v1',
    lastUploadGroupId: 'sp_last_upload_group_v1',
  };

  function isValidInstallID(id) {
    if (typeof id !== 'string' || id.length === 0 || id.length > 64) return false;
    return /^[A-Za-z0-9-]+$/.test(id);
  }

  function isValidSafeSegment(seg) {
    if (typeof seg !== 'string' || seg.length === 0 || seg.length > 255) return false;
    if (seg === '.' || seg === '..') return false;
    return !/[\/\\\x00]/.test(seg);
  }

  function parseSnapshotFilename(name) {
    const prefix = 'device_';
    const suffix = '.json.gz';
    if (typeof name !== 'string' || !name.startsWith(prefix) || !name.endsWith(suffix)) {
      return null;
    }
    const middle = name.slice(prefix.length, -suffix.length);
    const parts = middle.split('_');
    if (parts.length !== 2) return null;
    const [installId, revStr] = parts;
    if (!isValidInstallID(installId) || !/^[1-9][0-9]*$/.test(revStr)) return null;
    const rev = Number(revStr);
    if (!Number.isSafeInteger(rev) || rev <= 0 || String(rev) !== revStr) return null;
    return { installId, rev };
  }

  function snapshotFilename(installId, rev) {
    if (!isValidInstallID(installId)) throw new Error(`非法的 installId: ${installId}`);
    if (!Number.isSafeInteger(rev) || rev < 1) throw new Error(`非法的 rev: ${rev}`);
    return `device_${installId}_${rev}.json.gz`;
  }

  function parseInboxReadyFilename(name) {
    if (typeof name !== 'string' || !name.endsWith(INBOX_READY_SUFFIX)) return null;
    const id = name.slice(0, -INBOX_READY_SUFFIX.length);
    return isValidSafeSegment(id) ? id : null;
  }

  function strCmp(a, b) {
    const sa = a || '';
    const sb = b || '';
    if (sa < sb) return -1;
    if (sa > sb) return 1;
    return 0;
  }

  function normVersion(v) {
    if (!v || typeof v !== 'object') return { c: 0, i: '' };
    return { c: Number(v.c || 0), i: String(v.i || '') };
  }

  function compareVersion(a, b) {
    const va = normVersion(a);
    const vb = normVersion(b);
    if (va.c > vb.c) return 1;
    if (va.c < vb.c) return -1;
    return strCmp(va.i, vb.i);
  }

  function minPositive(a, b) {
    const na = Number(a || 0);
    const nb = Number(b || 0);
    if (na <= 0) return nb;
    if (nb <= 0) return na;
    return na < nb ? na : nb;
  }

  function checkSafeCounter(n, state) {
    const v = Number(n || 0);
    if (!Number.isSafeInteger(v) || v < 0) {
      state.unsafeInt = true;
      return state.max;
    }
    return v > state.max ? v : state.max;
  }

  function scanDataCounters(base, data) {
    const state = { max: 0, unsafeInt: false };
    state.max = checkSafeCounter(base, state);
    if (!data || typeof data !== 'object') return state;

    const scanVer = (v) => {
      if (v && typeof v === 'object') {
        state.max = checkSafeCounter(v.c, state);
      }
    };

    for (const g of data.groups || []) {
      scanVer(g.nameVer);
      scanVer(g.sortVer);
      scanVer(g.deletedVer);
    }
    for (const b of data.books || []) {
      scanVer(b.contentVer);
      scanVer(b.groupVer);
      scanVer(b.starredVer);
      scanVer(b.sortVer);
      scanVer(b.deletedVer);
    }
    for (const f of data.favorites || []) {
      scanVer(f.activeVer);
      scanVer(f.excerptVer);
    }
    for (const q of data.quotes || []) {
      scanVer(q.bodyVer);
      scanVer(q.favoriteVer);
      scanVer(q.deletedVer);
    }
    for (const p of data.progress || []) {
      scanVer(p.positionVer);
      scanVer(p.statusVer);
    }
    return state;
  }

  function mergePermanentDelete(xDel, xAt, xVer, yDel, yAt, yVer) {
    if (!xDel && !yDel) return { deleted: false, deletedAt: 0, deletedVer: { c: 0, i: '' } };
    if (xDel && !yDel) return { deleted: true, deletedAt: Number(xAt || 0), deletedVer: normVersion(xVer) };
    if (!xDel && yDel) return { deleted: true, deletedAt: Number(yAt || 0), deletedVer: normVersion(yVer) };
    const cmp = compareVersion(xVer, yVer);
    if (cmp > 0 || (cmp === 0 && Number(xAt || 0) >= Number(yAt || 0))) {
      return { deleted: true, deletedAt: Number(xAt || 0), deletedVer: normVersion(xVer) };
    }
    return { deleted: true, deletedAt: Number(yAt || 0), deletedVer: normVersion(yVer) };
  }

  function mergeOneGroup(x, y) {
    const out = {
      id: x.id,
      createdAt: minPositive(x.createdAt, y.createdAt),
    };
    const cmpName = compareVersion(x.nameVer, y.nameVer);
    if (cmpName > 0 || (cmpName === 0 && strCmp(x.name, y.name) >= 0)) {
      out.name = x.name || '';
      out.nameVer = normVersion(x.nameVer);
    } else {
      out.name = y.name || '';
      out.nameVer = normVersion(y.nameVer);
    }
    const cmpSort = compareVersion(x.sortVer, y.sortVer);
    if (cmpSort > 0 || (cmpSort === 0 && Number(x.sortOrder || 0) >= Number(y.sortOrder || 0))) {
      out.sortOrder = Number(x.sortOrder || 0);
      out.sortVer = normVersion(x.sortVer);
    } else {
      out.sortOrder = Number(y.sortOrder || 0);
      out.sortVer = normVersion(y.sortVer);
    }
    const del = mergePermanentDelete(
      !!x.deleted, x.deletedAt, x.deletedVer,
      !!y.deleted, y.deletedAt, y.deletedVer
    );
    if (del.deleted) {
      out.deleted = true;
      if (del.deletedAt) out.deletedAt = del.deletedAt;
      out.deletedVer = del.deletedVer;
    }
    return out;
  }

  function mergeGroups(a, b) {
    const map = new Map();
    const insert = (g) => {
      if (!g || !g.id) return;
      const cur = map.get(g.id);
      map.set(g.id, cur ? mergeOneGroup(cur, g) : cloneGroup(g));
    };
    for (const g of a || []) insert(g);
    for (const g of b || []) insert(g);
    return Array.from(map.values()).sort((x, y) => {
      if (x.sortOrder !== y.sortOrder) return x.sortOrder - y.sortOrder;
      return strCmp(x.id, y.id);
    });
  }

  function cloneGroup(g) {
    const out = {
      id: String(g.id),
      name: String(g.name || ''),
      nameVer: normVersion(g.nameVer),
      sortOrder: Number(g.sortOrder || 0),
      sortVer: normVersion(g.sortVer),
      createdAt: Number(g.createdAt || 0),
    };
    if (g.deleted) {
      out.deleted = true;
      if (g.deletedAt) out.deletedAt = Number(g.deletedAt);
      out.deletedVer = normVersion(g.deletedVer);
    }
    return out;
  }

  function cloneBook(b) {
    const out = {
      id: String(b.id),
      title: String(b.title || ''),
      author: String(b.author || ''),
      contentHash: String(b.contentHash || ''),
      contentSize: Number(b.contentSize || 0),
      rawFilename: String(b.rawFilename || ''),
      poemCount: Number(b.poemCount || 0),
      contentVer: normVersion(b.contentVer),
      isStarred: !!b.isStarred,
      sortOrder: Number(b.sortOrder || 0),
      createdAt: Number(b.createdAt || 0),
    };
    if (b.dynastyOrGenre) out.dynastyOrGenre = String(b.dynastyOrGenre);
    if (b.coverHash) out.coverHash = String(b.coverHash);
    if (b.contentPath) out.contentPath = String(b.contentPath);
    if (b.rawSize) out.rawSize = Number(b.rawSize);
    if (b.groupId) out.groupId = String(b.groupId);
    if (b.groupVer && (b.groupVer.c || b.groupVer.i)) out.groupVer = normVersion(b.groupVer);
    if (b.starredVer && (b.starredVer.c || b.starredVer.i)) out.starredVer = normVersion(b.starredVer);
    if (b.sortVer && (b.sortVer.c || b.sortVer.i)) out.sortVer = normVersion(b.sortVer);
    if (b.deleted) {
      out.deleted = true;
      if (b.deletedAt) out.deletedAt = Number(b.deletedAt);
      out.deletedVer = normVersion(b.deletedVer);
    }
    return out;
  }

  function mergeOneBook(x, y) {
    const out = {
      id: x.id,
      createdAt: minPositive(x.createdAt, y.createdAt),
    };
    let useXContent = false;
    const cmpContent = compareVersion(x.contentVer, y.contentVer);
    if (cmpContent > 0) {
      useXContent = true;
    } else if (cmpContent === 0) {
      if ((x.contentHash || '') !== (y.contentHash || '')) {
        useXContent = strCmp(x.contentHash, y.contentHash) > 0;
      } else if ((x.contentPath || '') !== (y.contentPath || '')) {
        useXContent = strCmp(x.contentPath, y.contentPath) > 0;
      } else {
        useXContent = strCmp(x.title, y.title) >= 0;
      }
    }
    const src = useXContent ? x : y;
    out.title = String(src.title || '');
    out.author = String(src.author || '');
    if (src.dynastyOrGenre) out.dynastyOrGenre = String(src.dynastyOrGenre);
    if (src.coverHash) out.coverHash = String(src.coverHash);
    out.contentHash = String(src.contentHash || '');
    out.contentSize = Number(src.contentSize || 0);
    if (src.contentPath) out.contentPath = String(src.contentPath);
    out.rawFilename = String(src.rawFilename || '');
    if (src.rawSize) out.rawSize = Number(src.rawSize);
    out.poemCount = Number(src.poemCount || 0);
    out.contentVer = normVersion(src.contentVer);

    const cmpGroup = compareVersion(x.groupVer, y.groupVer);
    if (cmpGroup > 0 || (cmpGroup === 0 && strCmp(x.groupId, y.groupId) >= 0)) {
      if (x.groupId) out.groupId = String(x.groupId);
      if (x.groupVer && (x.groupVer.c || x.groupVer.i)) out.groupVer = normVersion(x.groupVer);
    } else {
      if (y.groupId) out.groupId = String(y.groupId);
      if (y.groupVer && (y.groupVer.c || y.groupVer.i)) out.groupVer = normVersion(y.groupVer);
    }

    const cmpStar = compareVersion(x.starredVer, y.starredVer);
    if (cmpStar > 0 || (cmpStar === 0 && (!!x.isStarred || !y.isStarred))) {
      out.isStarred = !!x.isStarred;
      if (x.starredVer && (x.starredVer.c || x.starredVer.i)) out.starredVer = normVersion(x.starredVer);
    } else {
      out.isStarred = !!y.isStarred;
      if (y.starredVer && (y.starredVer.c || y.starredVer.i)) out.starredVer = normVersion(y.starredVer);
    }

    const cmpSort = compareVersion(x.sortVer, y.sortVer);
    if (cmpSort > 0 || (cmpSort === 0 && Number(x.sortOrder || 0) >= Number(y.sortOrder || 0))) {
      out.sortOrder = Number(x.sortOrder || 0);
      if (x.sortVer && (x.sortVer.c || x.sortVer.i)) out.sortVer = normVersion(x.sortVer);
    } else {
      out.sortOrder = Number(y.sortOrder || 0);
      if (y.sortVer && (y.sortVer.c || y.sortVer.i)) out.sortVer = normVersion(y.sortVer);
    }

    const del = mergePermanentDelete(
      !!x.deleted, x.deletedAt, x.deletedVer,
      !!y.deleted, y.deletedAt, y.deletedVer
    );
    if (del.deleted) {
      out.deleted = true;
      if (del.deletedAt) out.deletedAt = del.deletedAt;
      out.deletedVer = del.deletedVer;
    }
    return out;
  }

  function mergeBooks(a, b) {
    const map = new Map();
    const insert = (item) => {
      if (!item || !item.id) return;
      const cur = map.get(item.id);
      map.set(item.id, cur ? mergeOneBook(cur, item) : cloneBook(item));
    };
    for (const item of a || []) insert(item);
    for (const item of b || []) insert(item);
    return Array.from(map.values()).sort((x, y) => {
      if (x.sortOrder !== y.sortOrder) return x.sortOrder - y.sortOrder;
      return strCmp(x.id, y.id);
    });
  }

  /**
   * 构造 2A 累计稀疏快照：
   * - 继承油猴上一有效代的所有受管 groups / books 条目与永久墓碑
   * - 合入本次草稿修改后的完整 groups / books 条目
   * - 显式省略 favorites / quotes / progress / readingEvents，绝不污染移动端数据
   */
  function buildCumulativeSparseSnapshot({
    installId,
    nextRev,
    maxCounter,
    previousOwnData,
    mutatedGroups,
    mutatedBooks,
    nowMs,
  }) {
    if (!isValidInstallID(installId)) throw new Error('无效的 installId');
    if (!Number.isSafeInteger(nextRev) || nextRev < 1) throw new Error('无效的 nextRev');

    const prevGroups = (previousOwnData && previousOwnData.groups) || [];
    const prevBooks = (previousOwnData && previousOwnData.books) || [];
    const groups = mergeGroups(prevGroups, mutatedGroups || []);
    const books = mergeBooks(prevBooks, mutatedBooks || []);

    const data = {};
    if (groups.length > 0) data.groups = groups;
    if (books.length > 0) data.books = books;

    const scan = scanDataCounters(maxCounter, data);
    if (scan.unsafeInt) {
      throw new Error('检测到超出 JS 安全整数范围的 Lamport 计数，已中止写入');
    }

    return {
      schemaVersion: PROTOCOL_SCHEMA_VERSION,
      installId,
      rev: nextRev,
      maxCounter: scan.max,
      generatedAt: nowMs || Date.now(),
      data,
    };
  }

  function validateDecodedSnapshot(snap, expectedInstallId, expectedRev) {
    if (!snap || typeof snap !== 'object') throw new Error('快照 JSON 格式无效');
    if (snap.schemaVersion !== PROTOCOL_SCHEMA_VERSION) {
      throw new Error(`不支持的快照版本 schemaVersion=${snap.schemaVersion}`);
    }
    if (!isValidInstallID(snap.installId)) {
      throw new Error(`快照 installId 非法: ${snap.installId}`);
    }
    if (expectedInstallId && snap.installId !== expectedInstallId) {
      throw new Error(`快照 installId 不匹配: ${snap.installId} != ${expectedInstallId}`);
    }
    if (!Number.isSafeInteger(snap.rev) || snap.rev < 1) {
      throw new Error(`快照 rev 非法: ${snap.rev}`);
    }
    if (expectedRev && snap.rev !== expectedRev) {
      throw new Error(`快照 rev 不匹配: ${snap.rev} != ${expectedRev}`);
    }
    const scan = scanDataCounters(snap.maxCounter, snap.data);
    if (scan.unsafeInt) {
      return { snap, maxCounter: scan.max, unsafeInt: true };
    }
    return { snap, maxCounter: scan.max, unsafeInt: false };
  }

  function isZipMagic(bytes) {
    return bytes && bytes.length >= 4 &&
      bytes[0] === 0x50 && bytes[1] === 0x4b &&
      (bytes[2] === 0x03 || bytes[2] === 0x05 || bytes[2] === 0x07) &&
      (bytes[3] === 0x04 || bytes[3] === 0x06 || bytes[3] === 0x08);
  }

  function buildSafeRawFilename(originalFilename, shortRand) {
    const raw = String(originalFilename || 'book.epub').trim();
    const baseName = raw.split(/[\/\\]/).pop() || 'book.epub';
    const hasEpub = baseName.toLowerCase().endsWith('.epub');
    const stem = (hasEpub ? baseName.slice(0, -5) : baseName)
      .replace(/[\/\\\x00<>:"|?*]+/g, '_')
      .replace(/^\.+/, '')
      .trim()
      .slice(0, 80) || 'poetry';
    const rand = String(shortRand || randomHex(3)).replace(/[^a-zA-Z0-9]/g, '').slice(0, 8) || 'a1b2c3';
    return `${stem}_${rand}.epub`;
  }

  function randomHex(byteLen) {
    const arr = new Uint8Array(byteLen);
    if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
      crypto.getRandomValues(arr);
    } else {
      for (let i = 0; i < byteLen; i++) arr[i] = Math.floor(Math.random() * 256);
    }
    return Array.from(arr, (b) => b.toString(16).padStart(2, '0')).join('');
  }

  function newBookId() {
    const ts = Date.now().toString(36);
    return `sp-${ts}-${randomHex(4)}`;
  }

  function newInstallId() {
    return `tm-${randomHex(4)}`;
  }

  function bytesEqual(a, b) {
    if (!a || !b || a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) {
      if (a[i] !== b[i]) return false;
    }
    return true;
  }

  // 允许在 Node.js 单元测试中直接导入核心纯函数验证与 Go 协议一致性
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
      PROTOCOL_SCHEMA_VERSION,
      INBOX_SCHEMA_VERSION,
      INBOX_PARSER_RULE_VERSION,
      isValidInstallID,
      isValidSafeSegment,
      parseSnapshotFilename,
      snapshotFilename,
      parseInboxReadyFilename,
      compareVersion,
      scanDataCounters,
      mergeGroups,
      mergeBooks,
      buildCumulativeSparseSnapshot,
      validateDecodedSnapshot,
      isZipMagic,
      buildSafeRawFilename,
    };
    return;
  }

  // ============================================================================
  // 2. 浏览器端压缩 / SHA-256 与 WebDAV 客户端（GM_xmlhttpRequest 串行队列）
  // ============================================================================

  async function gzipUint8Array(rawBytes) {
    if (typeof CompressionStream === 'undefined') {
      throw new Error('当前浏览器不支持 CompressionStream(gzip)，请升级至新版 Chrome/Edge/Firefox');
    }
    const cs = new CompressionStream('gzip');
    const writer = cs.writable.getWriter();
    writer.write(rawBytes);
    writer.close();
    const buf = await new Response(cs.readable).arrayBuffer();
    return new Uint8Array(buf);
  }

  async function gunzipUint8ArrayLimited(compressedBytes, maxRawBytes) {
    if (typeof DecompressionStream === 'undefined') {
      throw new Error('当前浏览器不支持 DecompressionStream(gzip)');
    }
    const ds = new DecompressionStream('gzip');
    const writer = ds.writable.getWriter();
    const writePromise = writer.write(compressedBytes).then(() => writer.close());
    const reader = ds.readable.getReader();
    const chunks = [];
    let total = 0;
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > maxRawBytes) {
        try { await reader.cancel(); } catch { /* ignore cancel error */ }
        throw new Error(`解压体积超过上限 (${maxRawBytes} bytes)`);
      }
      chunks.push(value);
    }
    await writePromise;
    const out = new Uint8Array(total);
    let offset = 0;
    for (const c of chunks) {
      out.set(c, offset);
      offset += c.byteLength;
    }
    return out;
  }

  async function sha256Hex(uint8) {
    const hashBuf = await crypto.subtle.digest('SHA-256', uint8);
    return Array.from(new Uint8Array(hashBuf), (b) => b.toString(16).padStart(2, '0')).join('');
  }

  function encodeBasicAuth(username, password) {
    const raw = `${username}:${password}`;
    const bytes = new TextEncoder().encode(raw);
    let bin = '';
    for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
    return 'Basic ' + btoa(bin);
  }

  function sleep(ms) {
    return new Promise((r) => setTimeout(r, ms));
  }

  function parseRetryAfterMs(headersStr) {
    if (!headersStr) return 0;
    const match = headersStr.match(/^retry-after\s*:\s*(.+)$/im);
    if (!match) return 0;
    const val = match[1].trim();
    if (/^\d+$/.test(val)) {
      return Math.min(Number(val) * 1000, 30000);
    }
    const dateMs = Date.parse(val);
    if (!Number.isNaN(dateMs)) {
      return Math.max(0, Math.min(dateMs - Date.now(), 30000));
    }
    return 0;
  }

  class WebDAVClient {
    constructor(cfg) {
      this.baseURL = String(cfg.baseURL || '').trim().replace(/\/+$/, '');
      this.appFolder = String(cfg.appFolder || 'StillPoets').trim().replace(/^\/+|\/+$/g, '');
      this.username = String(cfg.username || '');
      this.password = String(cfg.password || '');
      if (!this.baseURL) throw new Error('请先填写 WebDAV 地址');
      const u = new URL(this.baseURL);
      if (u.protocol !== 'https:' && u.protocol !== 'http:') {
        throw new Error('WebDAV 地址必须以 https:// 或 http:// 开头');
      }
      this.origin = u.origin;
      this.authHeader = encodeBasicAuth(this.username, this.password);
      this._queue = Promise.resolve();
    }

    buildURL(relPath, isDir) {
      const parts = [];
      if (this.appFolder) {
        for (const seg of this.appFolder.split('/')) {
          if (seg) parts.push(encodeURIComponent(seg));
        }
      }
      if (relPath) {
        for (const seg of String(relPath).split('/')) {
          if (seg) parts.push(encodeURIComponent(seg));
        }
      }
      let full = this.baseURL + (parts.length ? '/' + parts.join('/') : '');
      if (isDir && !full.endsWith('/')) full += '/';
      return full;
    }

    enqueue(fn) {
      const next = this._queue.then(fn, fn);
      this._queue = next.catch(() => {});
      return next;
    }

    request({ method, url, headers = {}, data = null, responseType = 'text', isWrite = false }) {
      return this.enqueue(async () => {
        const maxAttempts = 4;
        for (let attempt = 0; attempt < maxAttempts; attempt++) {
          const res = await new Promise((resolve, reject) => {
            GM_xmlhttpRequest({
              method,
              url,
              headers: Object.assign({
                Authorization: this.authHeader,
                'Accept-Encoding': 'identity',
              }, headers),
              data,
              responseType,
              timeout: 90000,
              onload: resolve,
              onerror: (e) => reject(new Error(`网络错误: ${e.error || method}`)),
              ontimeout: () => reject(new Error(`请求超时: ${method} ${url}`)),
            });
          });

          // 写请求或 PROPFIND 严禁跨源重定向，防止把 Basic 凭据泄露给第三方域
          if (res.finalUrl) {
            try {
              const finalOrigin = new URL(res.finalUrl).origin;
              if (isWrite && finalOrigin !== this.origin) {
                throw new Error(`已拦截跨源写重定向 (${this.origin} -> ${finalOrigin})`);
              }
            } catch (e) {
              if (isWrite) throw e;
            }
          }

          if (res.status === 429 || res.status === 503) {
            if (attempt < maxAttempts - 1) {
              const wait = parseRetryAfterMs(res.responseHeaders) || (1000 * (attempt + 1));
              await sleep(wait);
              continue;
            }
          }
          return res;
        }
        throw new Error(`请求重试耗尽: ${method} ${url}`);
      });
    }

    async propfind(relPath, depth = '0', isDir = true) {
      const url = this.buildURL(relPath, isDir);
      const body = '<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/></d:prop></d:propfind>';
      const res = await this.request({
        method: 'PROPFIND',
        url,
        headers: {
          Depth: String(depth),
          'Content-Type': 'application/xml; charset=utf-8',
        },
        data: body,
        responseType: 'text',
        isWrite: true,
      });
      if (res.status === 404) return { notFound: true, entries: [] };
      if (res.status === 401 || res.status === 403) {
        throw new Error(`WebDAV 鉴权失败 (HTTP ${res.status})，请检查用户名与应用密码`);
      }
      if (res.status < 200 || res.status >= 300) {
        throw new Error(`PROPFIND ${relPath || '/'} 失败 (HTTP ${res.status})`);
      }
      return { notFound: false, entries: parsePropfindXML(res.responseText, url) };
    }

    async ensureDir(relPath) {
      const segments = String(relPath || '').split('/').filter(Boolean);
      const chain = [''];
      let cur = '';
      for (const s of segments) {
        cur = cur ? `${cur}/${s}` : s;
        chain.push(cur);
      }
      for (const dir of chain) {
        const url = this.buildURL(dir, true);
        const res = await this.request({ method: 'MKCOL', url, isWrite: true });
        if (res.status >= 200 && res.status < 300) continue;
        if (res.status === 405 || res.status === 301 || res.status === 409) {
          const probe = await this.propfind(dir, '0', true);
          if (!probe.notFound) continue;
        }
        throw new Error(`创建目录 ${dir || this.appFolder} 失败 (HTTP ${res.status})`);
      }
    }

    async listDir(relPath) {
      const r = await this.propfind(relPath, '1', true);
      if (r.notFound) return null;
      return r.entries;
    }

    async getBytes(relPath, maxBytes) {
      const url = this.buildURL(relPath, false);
      const res = await this.request({ method: 'GET', url, responseType: 'arraybuffer', isWrite: false });
      if (res.status === 404) return null;
      if (res.status < 200 || res.status >= 300) {
        throw new Error(`下载 ${relPath} 失败 (HTTP ${res.status})`);
      }
      const bytes = new Uint8Array(res.response || new ArrayBuffer(0));
      if (maxBytes && bytes.byteLength > maxBytes) {
        throw new Error(`远端文件 ${relPath} 超出体积上限 (${bytes.byteLength} > ${maxBytes})`);
      }
      return bytes;
    }

    async getBytesWithRetryAfterWrite(relPath, maxBytes) {
      const delays = [300, 700, 1500];
      for (let i = 0; ; i++) {
        const bytes = await this.getBytes(relPath, maxBytes);
        if (bytes !== null) return bytes;
        if (i >= delays.length) throw new Error(`写后回读 ${relPath} 仍返回 404`);
        await sleep(delays[i]);
      }
    }

    async putBytes(relPath, uint8, contentType = 'application/octet-stream') {
      const url = this.buildURL(relPath, false);
      const blob = new Blob([uint8], { type: contentType });
      const res = await this.request({
        method: 'PUT',
        url,
        headers: { 'Content-Type': contentType },
        data: blob,
        responseType: 'text',
        isWrite: true,
      });
      if (res.status < 200 || res.status >= 300) {
        throw new Error(`上传 ${relPath} 失败 (HTTP ${res.status})`);
      }
    }

    async deletePath(relPath, isDir = false) {
      const url = this.buildURL(relPath, isDir);
      const res = await this.request({ method: 'DELETE', url, isWrite: true });
      if (res.status === 404) return;
      if (res.status < 200 || res.status >= 300) {
        throw new Error(`删除 ${relPath} 失败 (HTTP ${res.status})`);
      }
    }
  }

  function decodePathSafe(p) {
    try {
      return decodeURIComponent(p);
    } catch {
      return p;
    }
  }

  function normalizePathname(href, baseUrl) {
    try {
      const u = new URL(href, baseUrl);
      return decodePathSafe(u.pathname).replace(/\/+$/, '');
    } catch {
      return decodePathSafe(String(href || '')).replace(/\/+$/, '');
    }
  }

  function parsePropfindXML(xmlText, requestURL) {
    const parser = new DOMParser();
    const doc = parser.parseFromString(xmlText || '', 'application/xml');
    const reqPath = normalizePathname(requestURL, requestURL);
    const responses = doc.getElementsByTagNameNS('*', 'response');
    const out = [];
    for (let i = 0; i < responses.length; i++) {
      const node = responses[i];
      const hrefEl = node.getElementsByTagNameNS('*', 'href')[0];
      if (!hrefEl || !hrefEl.textContent) continue;
      const entryPath = normalizePathname(hrefEl.textContent.trim(), requestURL);
      if (entryPath === reqPath) continue;
      const segs = entryPath.split('/').filter(Boolean);
      const name = segs.length ? segs[segs.length - 1] : '';
      if (!name) continue;
      const colEls = node.getElementsByTagNameNS('*', 'collection');
      const isDir = colEls && colEls.length > 0;
      const lenEl = node.getElementsByTagNameNS('*', 'getcontentlength')[0];
      const size = lenEl ? Number(lenEl.textContent.trim() || 0) : 0;
      out.push({ name, isDir, size });
    }
    return out;
  }

  // ============================================================================
  // 3. 书库同步引擎与投递逻辑（2A 累计稀疏快照 + inbox 投递）
  // ============================================================================

  function getOrCreateInstallId() {
    let id = GM_getValue(STORAGE_KEYS.installId, '');
    if (!isValidInstallID(id)) {
      id = newInstallId();
      GM_setValue(STORAGE_KEYS.installId, id);
      GM_setValue(STORAGE_KEYS.localRev, 0);
    }
    return id;
  }

  function loadSavedConfig() {
    return GM_getValue(STORAGE_KEYS.config, null);
  }

  function saveConfig(cfg) {
    GM_setValue(STORAGE_KEYS.config, {
      baseURL: String(cfg.baseURL || '').trim().replace(/\/+$/, ''),
      appFolder: String(cfg.appFolder || 'StillPoets').trim().replace(/^\/+|\/+$/g, '') || 'StillPoets',
      username: String(cfg.username || ''),
      password: String(cfg.password || ''),
    });
  }

  function loadZlibHosts() {
    const saved = GM_getValue(STORAGE_KEYS.zlibHosts, null);
    if (Array.isArray(saved) && saved.length > 0) return saved;
    return [
      'z-library.sk',
      'z-lib.gd',
      'z-lib.fm',
      'z-lib.gs',
      'singlelogin.re',
      'singlelogin.rs',
      '1lib.sk',
    ];
  }

  function saveZlibHosts(hosts) {
    GM_setValue(STORAGE_KEYS.zlibHosts, hosts);
  }

  class StillPoetsSyncSession {
    constructor(client) {
      this.client = client;
      this.installId = getOrCreateInstallId();
      this.localRev = Number(GM_getValue(STORAGE_KEYS.localRev, 0) || 0);
      this.maxCounter = Number(GM_getValue(STORAGE_KEYS.localCounter, 0) || 0);
      this.readOnlySafeInt = false;
      this.groups = [];
      this.books = [];
      this.inboxItems = [];
      this.ownPrevData = { groups: [], books: [] };
      this.ownRemoteRevs = [];
      this.ownValidRev = 0;
      this.remoteDeviceIds = [];
      this.warnings = [];
    }

    async ensureWorkspace() {
      await this.client.ensureDir(STATE_DIR);
      await this.client.ensureDir(BOOKS_DIR);
      await this.client.ensureDir(INBOX_DIR);
    }

    async loadAll() {
      this.warnings = [];
      this.readOnlySafeInt = false;
      this.ownPrevData = { groups: [], books: [] };
      this.ownRemoteRevs = [];
      this.ownValidRev = 0;
      this.remoteDeviceIds = [];

      const entries = await this.client.listDir(STATE_DIR);
      const byDevice = new Map();
      if (entries) {
        for (const e of entries) {
          if (e.isDir) continue;
          const parsed = parseSnapshotFilename(e.name);
          if (!parsed) continue;
          if (!byDevice.has(parsed.installId)) byDevice.set(parsed.installId, []);
          byDevice.get(parsed.installId).push(parsed.rev);
        }
      }
      for (const revs of byDevice.values()) {
        revs.sort((a, b) => b - a);
      }

      const deviceIds = Array.from(byDevice.keys()).sort(strCmp);
      let mergedGroups = [];
      let mergedBooks = [];
      let maxCounter = this.maxCounter;

      for (const devId of deviceIds) {
        const revs = byDevice.get(devId);
        if (devId === this.installId) {
          this.ownRemoteRevs = revs.slice();
        }
        let loaded = null;
        for (const rev of revs) {
          const relPath = `${STATE_DIR}/${snapshotFilename(devId, rev)}`;
          try {
            const compressed = await this.client.getBytes(relPath, MAX_SNAPSHOT_COMPRESSED_BYTES);
            if (!compressed) continue;
            const raw = await gunzipUint8ArrayLimited(compressed, MAX_SNAPSHOT_RAW_BYTES);
            const obj = JSON.parse(new TextDecoder('utf-8').decode(raw));
            const checked = validateDecodedSnapshot(obj, devId, rev);
            if (checked.unsafeInt) {
              this.readOnlySafeInt = true;
              this.warnings.push(`设备 ${devId} rev=${rev} 含超出 JS 安全整数范围的时钟，已自动切换为只读保护`);
            }
            if (checked.maxCounter > maxCounter) maxCounter = checked.maxCounter;
            loaded = { snap: checked.snap, rev };
            break;
          } catch (err) {
            this.warnings.push(`设备 ${devId} rev=${rev} 读取跳过: ${err.message}`);
          }
        }
        if (loaded) {
          if (devId === this.installId) {
            this.ownValidRev = loaded.rev;
            this.ownPrevData = {
              groups: (loaded.snap.data && loaded.snap.data.groups) || [],
              books: (loaded.snap.data && loaded.snap.data.books) || [],
            };
          } else {
            this.remoteDeviceIds.push(devId);
          }
          mergedGroups = mergeGroups(mergedGroups, (loaded.snap.data && loaded.snap.data.groups) || []);
          mergedBooks = mergeBooks(mergedBooks, (loaded.snap.data && loaded.snap.data.books) || []);
        }
      }

      this.groups = mergedGroups;
      this.books = mergedBooks;
      this.maxCounter = maxCounter;
      GM_setValue(STORAGE_KEYS.localCounter, maxCounter);

      // 缓存活跃分组，供 Z-Library 快捷浮层秒开选择
      const activeGroups = this.groups.filter((g) => !g.deleted).map((g) => ({ id: g.id, name: g.name }));
      GM_setValue(STORAGE_KEYS.cachedGroups, activeGroups);

      // 读取 inbox/*.ready.json
      await this.loadInbox();
    }

    async loadInbox() {
      this.inboxItems = [];
      const entries = await this.client.listDir(INBOX_DIR);
      if (!entries) return;
      const readyFiles = entries
        .filter((e) => !e.isDir && parseInboxReadyFilename(e.name))
        .sort((a, b) => strCmp(a.name, b.name));
      for (const e of readyFiles) {
        const relPath = `${INBOX_DIR}/${e.name}`;
        try {
          const raw = await this.client.getBytes(relPath, MAX_INBOX_READY_BYTES);
          if (!raw) continue;
          const item = JSON.parse(new TextDecoder('utf-8').decode(raw));
          this.inboxItems.push({ ...item, _readyPath: relPath });
        } catch (err) {
          this.warnings.push(`收件箱清单 ${e.name} 解析失败: ${err.message}`);
        }
      }
      this.inboxItems.sort((a, b) => Number(b.submittedAt || 0) - Number(a.submittedAt || 0));
    }

    nextVersion() {
      if (this.readOnlySafeInt || !Number.isSafeInteger(this.maxCounter + 1)) {
        throw new Error('时钟超出 JS 安全整数范围，当前会话为只读');
      }
      this.maxCounter += 1;
      return { c: this.maxCounter, i: this.installId };
    }

    async publishSparseMutations(mutatedGroups, mutatedBooks) {
      if (this.readOnlySafeInt) {
        throw new Error('当前工作区包含超出 JS 安全整数的时钟，已禁止写入');
      }
      await this.client.ensureDir(STATE_DIR);
      const ownRemoteMax = this.ownRemoteRevs.length > 0 ? this.ownRemoteRevs[0] : 0;
      const nextRev = Math.max(this.localRev, ownRemoteMax) + 1;

      const snap = buildCumulativeSparseSnapshot({
        installId: this.installId,
        nextRev,
        maxCounter: this.maxCounter,
        previousOwnData: this.ownPrevData,
        mutatedGroups,
        mutatedBooks,
        nowMs: Date.now(),
      });

      const rawJSON = new TextEncoder().encode(JSON.stringify(snap));
      if (rawJSON.byteLength > MAX_SNAPSHOT_RAW_BYTES) {
        throw new Error('快照 JSON 体积超出上限');
      }
      const compressed = await gzipUint8Array(rawJSON);
      if (compressed.byteLength > MAX_SNAPSHOT_COMPRESSED_BYTES) {
        throw new Error('快照压缩体积超出上限');
      }

      const relPath = `${STATE_DIR}/${snapshotFilename(this.installId, nextRev)}`;
      await this.client.putBytes(relPath, compressed, 'application/gzip');

      // 回读逐字节比对 + 解压验证
      const readBack = await this.client.getBytesWithRetryAfterWrite(relPath, MAX_SNAPSHOT_COMPRESSED_BYTES);
      if (!bytesEqual(readBack, compressed)) {
        throw new Error('快照写后回读字节不一致，可能是网盘进行了透明压缩或截断，已中止清理旧代次');
      }
      const decodedBack = JSON.parse(new TextDecoder('utf-8').decode(await gunzipUint8ArrayLimited(readBack, MAX_SNAPSHOT_RAW_BYTES)));
      validateDecodedSnapshot(decodedBack, this.installId, nextRev);

      this.localRev = nextRev;
      this.maxCounter = snap.maxCounter;
      GM_setValue(STORAGE_KEYS.localRev, nextRev);
      GM_setValue(STORAGE_KEYS.localCounter, snap.maxCounter);

      // 清理自己更早的旧代次，严格保留最近 2 份有效代次
      await this.cleanupOwnOldRevisions(nextRev);
      await this.loadAll();
    }

    async cleanupOwnOldRevisions(nextRev) {
      const keep = new Set([nextRev]);
      if (this.ownValidRev > 0 && this.ownValidRev < nextRev) {
        keep.add(this.ownValidRev);
      }
      for (const rev of this.ownRemoteRevs) {
        if (keep.size >= KEEP_SNAPSHOT_GENERATIONS) break;
        if (rev >= nextRev || keep.has(rev)) continue;
        try {
          const relPath = `${STATE_DIR}/${snapshotFilename(this.installId, rev)}`;
          const bytes = await this.client.getBytes(relPath, MAX_SNAPSHOT_COMPRESSED_BYTES);
          if (!bytes) continue;
          const obj = JSON.parse(new TextDecoder('utf-8').decode(await gunzipUint8ArrayLimited(bytes, MAX_SNAPSHOT_RAW_BYTES)));
          validateDecodedSnapshot(obj, this.installId, rev);
          keep.add(rev);
        } catch {
          // 跳过已损坏的旧代次
        }
      }
      if (keep.size < KEEP_SNAPSHOT_GENERATIONS) return;
      for (const rev of this.ownRemoteRevs) {
        if (keep.has(rev)) continue;
        try {
          await this.client.deletePath(`${STATE_DIR}/${snapshotFilename(this.installId, rev)}`, false);
        } catch { /* ignore cleanup error */ }
      }
    }

    async deliverEPUB(uint8, originalFilename, groupId, source = 'web-upload', onProgress = null) {
      if (!uint8 || uint8.byteLength === 0) throw new Error('EPUB 文件为空');
      if (uint8.byteLength > MAX_INBOX_EPUB_BYTES) {
        throw new Error(`文件过大（${formatBytes(uint8.byteLength)}），单本上限 64 MiB`);
      }
      if (!isZipMagic(uint8)) {
        throw new Error('文件不是有效的 EPUB/ZIP 格式（可能抓取到了登录页或限额提示页）');
      }

      if (onProgress) onProgress('正在计算 SHA-256 校验和…');
      const rawSha256 = await sha256Hex(uint8);
      const bookId = newBookId();
      const rawFilename = buildSafeRawFilename(originalFilename, randomHex(3));

      if (onProgress) onProgress('正在上传原始 EPUB 到 WebDAV…');
      await this.client.ensureDir(`${BOOKS_DIR}/${bookId}`);
      await this.client.putBytes(`${BOOKS_DIR}/${bookId}/${rawFilename}`, uint8, 'application/epub+zip');

      if (onProgress) onProgress('正在写入收件箱就绪清单…');
      await this.client.ensureDir(INBOX_DIR);

      const now = Date.now();
      const initVer = { c: Math.max(1, this.maxCounter + 1), i: this.installId };
      const manifest = {
        schemaVersion: INBOX_SCHEMA_VERSION,
        importId: bookId,
        bookId,
        originalFilename: String(originalFilename || rawFilename),
        rawFilename,
        rawSize: uint8.byteLength,
        rawSha256,
        groupId: groupId || '',
        initialGroupVer: initVer,
        initialStarredVer: initVer,
        initialSortVer: initVer,
        parserRuleVersion: INBOX_PARSER_RULE_VERSION,
        submittedAt: now,
        source,
      };
      const readyBytes = new TextEncoder().encode(JSON.stringify(manifest));
      await this.client.putBytes(`${INBOX_DIR}/${bookId}${INBOX_READY_SUFFIX}`, readyBytes, 'application/json');
      return manifest;
    }

    async cancelInboxItem(item) {
      if (item._readyPath) {
        await this.client.deletePath(item._readyPath, false);
      }
      if (item.bookId && isValidSafeSegment(item.bookId)) {
        // 仅清理未进入任何书架快照的待消费原件目录
        const known = this.books.some((b) => b.id === item.bookId);
        if (!known) {
          try {
            await this.client.deletePath(`${BOOKS_DIR}/${item.bookId}`, true);
          } catch { /* ignore best-effort cleanup */ }
        }
      }
      await this.loadInbox();
    }

    /**
     * 安全孤儿扫描：
     * - deletedBooks: 在 state/ 中明确具备 deleted === true 永久墓碑、且 books/ 下仍有目录的诗集（允许用户确认物理回收）
     * - unknownDirs: 不在任何快照中的 books/<id>/ 目录（可能是某台设备尚未发完快照的新书，仅只读展示，禁止删除）
     */
    async scanRemovableBookDirs() {
      const entries = await this.client.listDir(BOOKS_DIR);
      if (!entries) return { tombstoned: [], unknown: [] };
      const bookMap = new Map(this.books.map((b) => [b.id, b]));
      const inboxSet = new Set(this.inboxItems.map((i) => i.bookId));
      const tombstoned = [];
      const unknown = [];
      for (const e of entries) {
        if (!e.isDir || !isValidSafeSegment(e.name)) continue;
        const b = bookMap.get(e.name);
        if (b && b.deleted) {
          tombstoned.push({ id: b.id, title: b.title || b.id, author: b.author || '', rawFilename: b.rawFilename || '', rawSize: b.rawSize || 0 });
        } else if (!b && !inboxSet.has(e.name)) {
          unknown.push({ id: e.name });
        }
      }
      return { tombstoned, unknown };
    }

    async purgeTombstonedBookDir(bookId) {
      const b = this.books.find((x) => x.id === bookId);
      if (!b || !b.deleted) {
        throw new Error(`拒绝物理删除：书目 ${bookId} 未在已验证快照中标记为永久墓碑`);
      }
      await this.client.deletePath(`${BOOKS_DIR}/${bookId}`, true);
    }
  }

  // ============================================================================
  // 4. 纸书极简风全屏管理台 UI（仅在可信管理入口页渲染，Shadow DOM 彻底隔离）
  // ============================================================================

  function formatBytes(n) {
    const v = Number(n || 0);
    if (v <= 0) return '—';
    if (v < 1024) return `${v} B`;
    if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KB`;
    return `${(v / (1024 * 1024)).toFixed(2)} MB`;
  }

  function h(tag, props, ...children) {
    const el = document.createElement(tag);
    if (props) {
      for (const [k, v] of Object.entries(props)) {
        if (v === null || v === undefined || v === false) continue;
        if (k === 'className') el.className = v;
        else if (k === 'style') el.style.cssText = v;
        else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2).toLowerCase(), v);
        else if (k in el && k !== 'list' && k !== 'type') el[k] = v;
        else el.setAttribute(k, v === true ? '' : String(v));
      }
    }
    for (const child of children.flat(Infinity)) {
      if (child === null || child === undefined || child === false) continue;
      el.appendChild(typeof child === 'string' || typeof child === 'number' ? document.createTextNode(String(child)) : child);
    }
    return el;
  }

  function isManagerEntryPage() {
    return location.hostname === 'github.com' &&
      location.pathname.toLowerCase().startsWith('/sounfury/stillpoets') &&
      location.hash === MANAGER_HASH;
  }

  const MANAGER_CSS = `
  :host { all: initial; font-family: -apple-system, BlinkMacSystemFont, "PingFang SC", "Noto Serif SC", "Microsoft YaHei", sans-serif; color: #1C1B19; }
  * { box-sizing: border-box; }
  .shell { display: flex; flex-direction: column; height: 100vh; background: #F7F6F1; }
  .topbar { height: 58px; padding: 0 20px; display: flex; align-items: center; justify-content: space-between; background: #FFFFFF; border-bottom: 1px solid #E6E3DA; flex-shrink: 0; gap: 12px; }
  .brand { display: flex; align-items: center; gap: 10px; font-weight: 600; font-size: 16px; letter-spacing: 0.3px; }
  .brand-mark { width: 26px; height: 26px; border-radius: 7px; background: #BC5B46; color: #FFF; display: grid; place-items: center; font-size: 13px; }
  .sub-pill { font-size: 12px; color: #6E6A61; background: #F2F0E8; padding: 3px 9px; border-radius: 99px; font-weight: 400; }
  .actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  button { font: inherit; font-size: 13px; padding: 6px 12px; border-radius: 8px; border: 1px solid #DDD8CC; background: #FFFFFF; color: #2A2824; cursor: pointer; transition: all .15s; }
  button:hover { background: #F4F2EA; border-color: #C9C3B4; }
  button:disabled { opacity: .48; cursor: not-allowed; }
  button.primary { background: #BC5B46; color: #FFF; border-color: #BC5B46; }
  button.primary:hover { background: #A44C39; }
  button.danger { color: #B4382A; border-color: #E7C3BD; }
  button.danger:hover { background: #FDF2F0; }
  button.ghost { border-color: transparent; background: transparent; padding: 4px 8px; }
  button.ghost:hover { background: #EFECE3; }
  .workspace { display: flex; flex: 1; min-height: 0; }
  .sidebar { width: 248px; background: #FAF9F5; border-right: 1px solid #E6E3DA; padding: 16px 12px; display: flex; flex-direction: column; gap: 6px; overflow-y: auto; flex-shrink: 0; }
  .nav-section { font-size: 11px; color: #8B8579; padding: 10px 10px 4px; letter-spacing: 0.8px; text-transform: uppercase; display: flex; justify-content: space-between; align-items: center; }
  .nav-item { display: flex; align-items: center; justify-content: space-between; gap: 6px; padding: 8px 10px; border-radius: 8px; cursor: pointer; font-size: 13.5px; color: #3A3731; }
  .nav-item:hover { background: #EFECE3; }
  .nav-item.active { background: #ECE7DA; color: #1C1B19; font-weight: 600; }
  .nav-count { font-size: 12px; color: #8B8579; }
  .grp-row { display: flex; align-items: center; gap: 4px; }
  .grp-row .nav-item { flex: 1; min-width: 0; }
  .grp-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .grp-ops { display: none; gap: 2px; }
  .grp-row:hover .grp-ops { display: flex; }
  .mini-btn { padding: 2px 5px; font-size: 11px; border-radius: 5px; }
  .main { flex: 1; display: flex; flex-direction: column; min-width: 0; padding: 18px 24px; overflow-y: auto; gap: 14px; }
  .dropzone { border: 1.5px dashed #CFC9BA; border-radius: 12px; padding: 14px 18px; background: #FFFFFF; display: flex; align-items: center; justify-content: space-between; gap: 12px; transition: all .15s; }
  .dropzone.dragover { border-color: #BC5B46; background: #FDF6F3; }
  .toolbar { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
  .search { width: 280px; max-width: 100%; padding: 7px 12px; border-radius: 8px; border: 1px solid #DDD8CC; background: #FFF; font-size: 13px; color: #1C1B19; }
  .batch-bar { display: flex; align-items: center; gap: 8px; padding: 8px 14px; border-radius: 10px; background: #F1ECE1; border: 1px solid #DFD8C8; font-size: 13px; }
  select, input[type="text"], input[type="password"], textarea { font: inherit; font-size: 13px; padding: 6px 10px; border-radius: 7px; border: 1px solid #D8D3C6; background: #FFF; color: #1C1B19; }
  .card { background: #FFFFFF; border: 1px solid #E6E3DA; border-radius: 12px; overflow: hidden; }
  table { width: 100%; border-collapse: collapse; font-size: 13.5px; }
  th { text-align: left; font-weight: 500; font-size: 12px; color: #7C776C; background: #FAF9F5; padding: 10px 12px; border-bottom: 1px solid #E8E5DC; }
  td { padding: 10px 12px; border-bottom: 1px solid #F1EFE8; vertical-align: middle; }
  tr:last-child td { border-bottom: none; }
  tr:hover td { background: #FCFAF5; }
  .title-cell { font-weight: 500; color: #1C1B19; display: flex; align-items: center; gap: 6px; }
  .meta-sub { font-size: 12px; color: #8B8579; margin-top: 2px; }
  .star-btn { font-size: 15px; color: #C2BCB0; border: none; background: transparent; padding: 2px 6px; }
  .star-btn.on { color: #BC5B46; }
  .empty { padding: 48px 20px; text-align: center; color: #8B8579; font-size: 14px; }
  .warn-banner { padding: 10px 14px; border-radius: 10px; background: #FFF7E8; border: 1px solid #ECD6A8; color: #7A5412; font-size: 12.5px; }
  .upload-log { font-size: 12.5px; padding: 8px 12px; border-radius: 8px; background: #FAF9F5; border: 1px solid #E6E3DA; display: flex; flex-direction: column; gap: 4px; }
  .toast { position: fixed; bottom: 22px; right: 24px; padding: 10px 16px; border-radius: 10px; background: #1F1E1C; color: #FFF; font-size: 13px; box-shadow: 0 8px 24px rgba(0,0,0,.16); z-index: 20; }
  .toast.ok { background: #2F6846; }
  .toast.err { background: #A63628; }
  .modal-mask { position: fixed; inset: 0; background: rgba(20,18,16,.38); display: grid; place-items: center; z-index: 15; padding: 16px; }
  .modal { width: 520px; max-width: 100%; max-height: 86vh; overflow-y: auto; background: #FFF; border-radius: 14px; padding: 20px 22px; box-shadow: 0 18px 48px rgba(0,0,0,.18); }
  .modal h3 { margin: 0 0 14px; font-size: 16px; }
  .field { display: flex; flex-direction: column; gap: 5px; margin-bottom: 12px; font-size: 13px; }
  .field label { color: #5C574E; font-weight: 500; }
  .modal-foot { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
  .hint { font-size: 12px; color: #7C776C; line-height: 1.5; }
  .cleanup-row { display: flex; justify-content: space-between; align-items: center; padding: 8px 10px; border: 1px solid #E6E3DA; border-radius: 8px; }
  `;

  function mountManagerApp() {
    if (document.getElementById('stillpoets-manager-root')) return;

    const host = document.createElement('div');
    host.id = 'stillpoets-manager-root';
    host.style.cssText = 'position:fixed;inset:0;z-index:2147483646;background:#F7F6F1;';
    document.body.appendChild(host);
    const shadow = host.attachShadow({ mode: 'open' });
    const styleEl = document.createElement('style');
    styleEl.textContent = MANAGER_CSS;

    const state = {
      session: null,
      loading: false,
      saving: false,
      filterScope: 'all',
      search: '',
      selectedIds: new Set(),
      draftGroups: new Map(),
      draftBooks: new Map(),
      modal: null,
      cleanupData: null,
      toast: null,
      uploadLogs: [],
    };

    let toastTimer = null;
    function showToast(msg, kind = 'info') {
      state.toast = { msg, kind };
      clearTimeout(toastTimer);
      render();
      toastTimer = setTimeout(() => {
        state.toast = null;
        render();
      }, 3600);
    }

    function effectiveGroups() {
      const base = (state.session && state.session.groups) || [];
      return mergeGroups(base, Array.from(state.draftGroups.values())).filter((g) => !g.deleted);
    }

    function effectiveBooks() {
      const base = (state.session && state.session.books) || [];
      const activeGroupIds = new Set(effectiveGroups().map((g) => g.id));
      return mergeBooks(base, Array.from(state.draftBooks.values()))
        .filter((b) => !b.deleted)
        .map((b) => (b.groupId && !activeGroupIds.has(b.groupId) ? { ...b, groupId: '' } : b));
    }

    function findMergedBook(bookId) {
      if (state.draftBooks.has(bookId)) return cloneBook(state.draftBooks.get(bookId));
      const base = ((state.session && state.session.books) || []).find((b) => b.id === bookId);
      return base ? cloneBook(base) : null;
    }

    function findMergedGroup(groupId) {
      if (state.draftGroups.has(groupId)) return cloneGroup(state.draftGroups.get(groupId));
      const base = ((state.session && state.session.groups) || []).find((g) => g.id === groupId);
      return base ? cloneGroup(base) : null;
    }

    function dirtyCount() {
      return state.draftGroups.size + state.draftBooks.size;
    }

    function ensureWritable() {
      if (!state.session) {
        showToast('请先配置并连接 WebDAV', 'err');
        state.modal = 'config';
        render();
        return false;
      }
      if (state.session.readOnlySafeInt) {
        showToast('检测到超出 JS 安全整数的时钟，当前会话仅只读', 'err');
        return false;
      }
      return true;
    }

    async function connectAndRefresh() {
      const cfg = loadSavedConfig();
      if (!cfg || !cfg.baseURL) {
        state.modal = 'config';
        render();
        return;
      }
      state.loading = true;
      render();
      try {
        const client = new WebDAVClient(cfg);
        const session = new StillPoetsSyncSession(client);
        await session.loadAll();
        state.session = session;
        state.draftGroups.clear();
        state.draftBooks.clear();
        state.selectedIds.clear();
        showToast(`已同步：${effectiveBooks().length} 本诗集，${session.inboxItems.length} 本待入库`, 'ok');
      } catch (err) {
        showToast(`连接失败：${err.message}`, 'err');
      } finally {
        state.loading = false;
        render();
      }
    }

    async function commitDrafts() {
      if (!ensureWritable()) return;
      if (dirtyCount() === 0) {
        showToast('当前没有待保存的修改');
        return;
      }
      state.saving = true;
      render();
      try {
        await state.session.publishSparseMutations(
          Array.from(state.draftGroups.values()),
          Array.from(state.draftBooks.values())
        );
        state.draftGroups.clear();
        state.draftBooks.clear();
        state.selectedIds.clear();
        showToast('已发布新快照，打开手机/平板同步即可无感生效', 'ok');
      } catch (err) {
        showToast(`发布失败：${err.message}`, 'err');
      } finally {
        state.saving = false;
        render();
      }
    }

    function createGroup(name) {
      if (!ensureWritable()) return;
      const trimmed = String(name || '').trim();
      if (!trimmed) return;
      const maxOrder = effectiveGroups().reduce((m, g) => Math.max(m, g.sortOrder), -1);
      const ver = state.session.nextVersion();
      const g = {
        id: `grp-${Date.now().toString(36)}-${randomHex(3)}`,
        name: trimmed,
        nameVer: ver,
        sortOrder: maxOrder + 1,
        sortVer: ver,
        createdAt: Date.now(),
      };
      state.draftGroups.set(g.id, g);
      render();
    }

    function renameGroup(groupId, newName) {
      if (!ensureWritable()) return;
      const trimmed = String(newName || '').trim();
      if (!trimmed) return;
      const g = findMergedGroup(groupId);
      if (!g || g.deleted) return;
      g.name = trimmed;
      g.nameVer = state.session.nextVersion();
      state.draftGroups.set(g.id, g);
      render();
    }

    function moveGroupOrder(groupId, delta) {
      if (!ensureWritable()) return;
      const list = effectiveGroups();
      const idx = list.findIndex((g) => g.id === groupId);
      const target = idx + delta;
      if (idx < 0 || target < 0 || target >= list.length) return;
      const reordered = list.slice();
      const [moved] = reordered.splice(idx, 1);
      reordered.splice(target, 0, moved);
      const ver = state.session.nextVersion();
      reordered.forEach((item, i) => {
        if (item.sortOrder !== i) {
          const g = findMergedGroup(item.id);
          if (g) {
            g.sortOrder = i;
            g.sortVer = ver;
            state.draftGroups.set(g.id, g);
          }
        }
      });
      render();
    }

    function dissolveGroup(groupId) {
      if (!ensureWritable()) return;
      const g = findMergedGroup(groupId);
      if (!g || g.deleted) return;
      if (!confirm(`确认解散分组「${g.name}」？组内诗集将自动归入默认「全部诗集」。`)) return;
      const ver = state.session.nextVersion();
      const now = Date.now();
      g.deleted = true;
      g.deletedAt = now;
      g.deletedVer = ver;
      state.draftGroups.set(g.id, g);
      for (const b of effectiveBooks()) {
        if (b.groupId === groupId) {
          const book = findMergedBook(b.id);
          if (book) {
            delete book.groupId;
            book.groupVer = ver;
            state.draftBooks.set(book.id, book);
          }
        }
      }
      if (state.filterScope === `group:${groupId}`) state.filterScope = 'all';
      render();
    }

    function setBooksGroup(bookIds, groupId) {
      if (!ensureWritable()) return;
      const ver = state.session.nextVersion();
      for (const id of bookIds) {
        const b = findMergedBook(id);
        if (!b || b.deleted) continue;
        if (groupId) b.groupId = groupId;
        else delete b.groupId;
        b.groupVer = ver;
        state.draftBooks.set(b.id, b);
      }
      render();
    }

    function toggleStar(bookId) {
      if (!ensureWritable()) return;
      const b = findMergedBook(bookId);
      if (!b || b.deleted) return;
      b.isStarred = !b.isStarred;
      b.starredVer = state.session.nextVersion();
      state.draftBooks.set(b.id, b);
      render();
    }

    function softDeleteBooks(bookIds) {
      if (!ensureWritable()) return;
      if (!bookIds.length) return;
      if (!confirm(`确认移除选中的 ${bookIds.length} 本诗集？（将发布永久软删墓碑；如需彻底释放网盘空间，可在顶部「清理已删原件」确认回收）`)) return;
      const ver = state.session.nextVersion();
      const now = Date.now();
      for (const id of bookIds) {
        const b = findMergedBook(id);
        if (!b || b.deleted) continue;
        b.deleted = true;
        b.deletedAt = now;
        b.deletedVer = ver;
        state.draftBooks.set(b.id, b);
        state.selectedIds.delete(id);
      }
      render();
    }

    async function handleEPUBFiles(fileList) {
      if (!ensureWritable()) return;
      const files = Array.from(fileList || []).filter((f) => f.name.toLowerCase().endsWith('.epub'));
      if (!files.length) {
        showToast('请选择 .epub 格式的诗集文件', 'err');
        return;
      }
      const targetGroup = state.filterScope.startsWith('group:') ? state.filterScope.slice(6) : '';
      for (const file of files) {
        const logItem = { name: file.name, status: '读取中…', ok: null };
        state.uploadLogs.unshift(logItem);
        render();
        try {
          const buf = new Uint8Array(await file.arrayBuffer());
          await state.session.deliverEPUB(buf, file.name, targetGroup, 'web-upload', (msg) => {
            logItem.status = msg;
            render();
          });
          logItem.status = '已投递到收件箱，打开手机/平板同步即自动入库';
          logItem.ok = true;
        } catch (err) {
          logItem.status = `失败：${err.message}`;
          logItem.ok = false;
        }
        render();
      }
      await state.session.loadInbox();
      render();
    }

    async function openCleanupModal() {
      if (!state.session) {
        showToast('请先连接 WebDAV', 'err');
        return;
      }
      state.modal = 'cleanup';
      state.cleanupData = null;
      render();
      try {
        state.cleanupData = await state.session.scanRemovableBookDirs();
      } catch (err) {
        state.cleanupData = { error: err.message, tombstoned: [], unknown: [] };
      }
      render();
    }

    function buildInboxCard(inbox, groupNameMap) {
      if (!inbox.length) {
        return h('div', { className: 'card' },
          h('div', { className: 'empty' }, '收件箱为空。拖入 EPUB 或在 Z-Library 点击一键投递后，未入库诗集会显示在这里。')
        );
      }
      return h('div', { className: 'card' },
        h('table', null,
          h('thead', null, h('tr', null,
            h('th', null, '原始文件'), h('th', null, '目标分组'), h('th', null, '大小'),
            h('th', null, '来源'), h('th', null, '投递时间'), h('th', null, '操作')
          )),
          h('tbody', null, inbox.map((item) => h('tr', null,
            h('td', null,
              h('div', { className: 'title-cell' }, item.originalFilename || item.rawFilename),
              h('div', { className: 'meta-sub' }, item.bookId)
            ),
            h('td', null, groupNameMap.get(item.groupId) || '全部诗集'),
            h('td', null, formatBytes(item.rawSize)),
            h('td', null, item.source || 'web'),
            h('td', null, item.submittedAt ? new Date(item.submittedAt).toLocaleString() : '—'),
            h('td', null, h('button', {
              className: 'danger mini-btn',
              onClick: async () => {
                if (!confirm(`确认撤销待入库任务「${item.originalFilename}」？`)) return;
                try {
                  await state.session.cancelInboxItem(item);
                  showToast('已撤销收件箱任务', 'ok');
                } catch (err) {
                  showToast(`撤销失败：${err.message}`, 'err');
                }
                render();
              },
            }, '撤销'))
          )))
        )
      );
    }

    function buildBooksSection(visibleBooks, groups) {
      const selectedCount = visibleBooks.filter((b) => state.selectedIds.has(b.id)).length;
      const searchInput = h('input', {
        id: 'search-input',
        className: 'search',
        type: 'text',
        placeholder: '搜索诗集名、作者、文件名…',
        value: state.search,
        onInput: (e) => {
          state.search = e.target.value;
          const pos = e.target.selectionStart;
          render();
          const re = shadow.getElementById('search-input');
          if (re) { re.focus(); re.setSelectionRange(pos, pos); }
        },
      });

      const batchBar = selectedCount > 0 ? h('div', { className: 'batch-bar' },
        h('span', null, '已选 ', h('strong', null, String(selectedCount)), ' 本'),
        h('select', {
          onChange: (e) => {
            if (e.target.value === '__noop__') return;
            const ids = visibleBooks.filter((b) => state.selectedIds.has(b.id)).map((b) => b.id);
            setBooksGroup(ids, e.target.value);
            state.selectedIds.clear();
          },
        },
          h('option', { value: '__noop__' }, '移动到分组…'),
          h('option', { value: '' }, '移出分组（全部诗集）'),
          groups.map((g) => h('option', { value: g.id }, g.name))
        ),
        h('button', {
          className: 'danger',
          onClick: () => softDeleteBooks(visibleBooks.filter((b) => state.selectedIds.has(b.id)).map((b) => b.id)),
        }, '批量删除')
      ) : null;

      const tableOrEmpty = visibleBooks.length === 0
        ? h('div', { className: 'empty' }, state.session ? '当前筛选下暂无诗集' : '请先点击右上角「WebDAV 设置」连接你的书库')
        : h('table', null,
            h('thead', null, h('tr', null,
              h('th', { style: 'width:36px' }, h('input', {
                type: 'checkbox',
                checked: selectedCount === visibleBooks.length && visibleBooks.length > 0,
                onChange: (e) => {
                  if (e.target.checked) visibleBooks.forEach((b) => state.selectedIds.add(b.id));
                  else visibleBooks.forEach((b) => state.selectedIds.delete(b.id));
                  render();
                },
              })),
              h('th', { style: 'width:40px' }, '星标'),
              h('th', null, '诗集 / 作者'),
              h('th', { style: 'width:180px' }, '分组'),
              h('th', { style: 'width:90px' }, '篇数'),
              h('th', { style: 'width:95px' }, '原件'),
              h('th', { style: 'width:76px' }, '操作')
            )),
            h('tbody', null, visibleBooks.map((b) => h('tr', null,
              h('td', null, h('input', {
                type: 'checkbox',
                checked: state.selectedIds.has(b.id),
                onChange: (e) => {
                  if (e.target.checked) state.selectedIds.add(b.id);
                  else state.selectedIds.delete(b.id);
                  render();
                },
              })),
              h('td', null, h('button', {
                className: `star-btn${b.isStarred ? ' on' : ''}`,
                onClick: () => toggleStar(b.id),
              }, b.isStarred ? '★' : '☆')),
              h('td', null,
                h('div', { className: 'title-cell' }, b.title, state.draftBooks.has(b.id) ? h('span', { className: 'sub-pill' }, '未保存') : null),
                h('div', { className: 'meta-sub' }, `${b.author || '佚名'} · ${b.rawFilename || ''}`)
              ),
              h('td', null, h('select', {
                onChange: (e) => setBooksGroup([b.id], e.target.value),
              },
                h('option', { value: '', selected: !b.groupId }, '全部诗集'),
                groups.map((g) => h('option', { value: g.id, selected: b.groupId === g.id }, g.name))
              )),
              h('td', null, `${b.poemCount || 0} 首`),
              h('td', null, formatBytes(b.rawSize || b.contentSize)),
              h('td', null, h('button', { className: 'danger mini-btn', onClick: () => softDeleteBooks([b.id]) }, '删除'))
            )))
          );

      return [
        h('div', { className: 'toolbar' }, searchInput, batchBar),
        h('div', { className: 'card' }, tableOrEmpty),
      ];
    }

    function buildModal() {
      const close = () => { state.modal = null; render(); };
      const wrap = (content) => h('div', {
        className: 'modal-mask',
        onClick: (e) => { if (e.target === e.currentTarget) close(); },
      }, h('div', { className: 'modal' }, content));

      if (state.modal === 'config') {
        const cfg = loadSavedConfig() || { baseURL: '', appFolder: 'StillPoets', username: '', password: '' };
        const urlIn = h('input', { type: 'text', placeholder: 'https://dav.jianguoyun.com/dav 或 https://alist.example.com/dav/115', value: cfg.baseURL });
        const folderIn = h('input', { type: 'text', value: cfg.appFolder || 'StillPoets' });
        const userIn = h('input', { type: 'text', value: cfg.username });
        const passIn = h('input', { type: 'password', value: cfg.password });
        return wrap([
          h('h3', null, 'WebDAV 连接配置'),
          h('div', { className: 'field' }, h('label', null, 'WebDAV 服务地址'), urlIn),
          h('div', { className: 'field' }, h('label', null, '应用子目录（默认 StillPoets）'), folderIn),
          h('div', { className: 'field' }, h('label', null, '用户名'), userIn),
          h('div', { className: 'field' }, h('label', null, '应用密码（仅保存在油猴隔离存储，建议填写专用应用密码）'), passIn),
          h('div', { className: 'hint' }, '安全边界：仅在此独立管理入口填写 WebDAV 凭据，脚本不会在 Z-Library 等第三方页面暴露凭据输入框。'),
          h('div', { className: 'modal-foot' },
            h('button', {
              className: 'danger',
              onClick: () => {
                GM_deleteValue(STORAGE_KEYS.config);
                state.session = null;
                state.draftGroups.clear();
                state.draftBooks.clear();
                state.modal = null;
                showToast('已清除本地 WebDAV 凭据');
                render();
              },
            }, '清除本地凭据'),
            h('button', { onClick: close }, '取消'),
            h('button', {
              className: 'primary',
              onClick: async () => {
                try {
                  saveConfig({ baseURL: urlIn.value, appFolder: folderIn.value, username: userIn.value, password: passIn.value });
                  state.modal = null;
                  await connectAndRefresh();
                  if (state.session) await state.session.ensureWorkspace();
                } catch (err) {
                  showToast(`保存失败：${err.message}`, 'err');
                }
              },
            }, '保存并连接')
          ),
        ]);
      }

      if (state.modal === 'zlib') {
        const ta = h('textarea', { rows: '8' }, loadZlibHosts().join('\n'));
        return wrap([
          h('h3', null, 'Z-Library 域名白名单'),
          h('div', { className: 'field' }, h('label', null, '每行一个域名（支持填你的 Z-Library 个人专属域名）'), ta),
          h('div', { className: 'hint' }, '在匹配域名的书籍详情页，脚本会在右下角展示一个轻量「发送到 StillPoets」按钮，直接复用当前浏览器登录态抓取 EPUB 并投递到你的 WebDAV 收件箱。'),
          h('div', { className: 'modal-foot' },
            h('button', { onClick: close }, '取消'),
            h('button', {
              className: 'primary',
              onClick: () => {
                const lines = ta.value.split(/\r?\n/).map((s) => s.trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/.*$/, '')).filter(Boolean);
                saveZlibHosts(Array.from(new Set(lines)));
                state.modal = null;
                showToast(`已保存 ${lines.length} 个 Z-Library 域名`, 'ok');
                render();
              },
            }, '保存域名')
          ),
        ]);
      }

      if (state.modal === 'cleanup') {
        const data = state.cleanupData;
        const body = [];
        if (!data) {
          body.push(h('div', { className: 'hint' }, '正在扫描 WebDAV books/ 目录…'));
        } else if (data.error) {
          body.push(h('div', { className: 'warn-banner' }, `扫描失败：${data.error}`));
        } else {
          body.push(h('div', { className: 'hint', style: 'margin-bottom:10px' }, '仅允许物理删除在已验证快照中明确带有永久删除墓碑的诗集目录。物理清理将永久移除 WebDAV 中的原始 EPUB 恢复源。'));
          if (!data.tombstoned.length) {
            body.push(h('div', { className: 'hint' }, '暂无可回收的已软删诗集原件。'));
          } else {
            body.push(h('div', { style: 'display:flex;flex-direction:column;gap:8px;margin-bottom:14px' },
              data.tombstoned.map((t) => h('div', { className: 'cleanup-row' },
                h('div', null, h('strong', null, t.title), h('div', { className: 'meta-sub' }, `${t.rawFilename || t.id} · ${formatBytes(t.rawSize)}`)),
                h('button', {
                  className: 'danger mini-btn',
                  onClick: async () => {
                    if (!confirm(`确定彻底删除 WebDAV 中 ${t.id} 的原始 EPUB 与缓存文件？此操作不可恢复！`)) return;
                    try {
                      await state.session.purgeTombstonedBookDir(t.id);
                      showToast(`已彻底清理 ${t.id}`, 'ok');
                      await openCleanupModal();
                    } catch (err) {
                      showToast(`清理失败：${err.message}`, 'err');
                    }
                  },
                }, '彻底删除原件')
              ))
            ));
          }
          if (data.unknown.length) {
            body.push(h('div', { className: 'warn-banner' }, `发现 ${data.unknown.length} 个未在当前快照中出现的目录（可能是其他设备上传中尚未发快照的新书，为防误删仅只读列出）：${data.unknown.map((u) => u.id).join(', ')}`));
          }
        }
        return wrap([
          h('h3', null, '清理已软删诗集的远端原件'),
          ...body,
          h('div', { className: 'modal-foot' }, h('button', { onClick: close }, '关闭')),
        ]);
      }
      return null;
    }

    function render() {
      const groups = effectiveGroups();
      const allBooks = effectiveBooks();
      const inbox = (state.session && state.session.inboxItems) || [];
      const groupNameMap = new Map(groups.map((g) => [g.id, g.name]));
      const dirty = dirtyCount();
      const q = state.search.trim().toLowerCase();
      const visibleBooks = allBooks.filter((b) => {
        if (state.filterScope === 'starred' && !b.isStarred) return false;
        if (state.filterScope.startsWith('group:') && (b.groupId || '') !== state.filterScope.slice(6)) return false;
        if (q && !`${b.title} ${b.author} ${b.rawFilename}`.toLowerCase().includes(q)) return false;
        return true;
      });

      const epubInput = h('input', {
        type: 'file',
        accept: '.epub',
        multiple: true,
        style: 'display:none',
        onChange: (e) => { handleEPUBFiles(e.target.files); e.target.value = ''; },
      });
      const dropzone = h('div', {
        className: 'dropzone',
        onDragover: (e) => { e.preventDefault(); dropzone.classList.add('dragover'); },
        onDragleave: () => dropzone.classList.remove('dragover'),
        onDrop: (e) => {
          e.preventDefault();
          dropzone.classList.remove('dragover');
          if (e.dataTransfer && e.dataTransfer.files) handleEPUBFiles(e.dataTransfer.files);
        },
      },
        h('div', null,
          h('strong', null, '拖入 EPUB 诗集投递到 WebDAV'),
          h('div', { className: 'hint' },
            `先直传原件至 books/ 再写入 inbox/ 就绪清单；打开任意手机/平板同步即自动解析入库。${state.filterScope.startsWith('group:') ? `当前将自动归入「${groupNameMap.get(state.filterScope.slice(6)) || ''}」。` : ''}`
          )
        ),
        h('div', null, epubInput, h('button', { onClick: () => epubInput.click() }, '选择 EPUB 文件'))
      );

      const navItem = (label, count, scope) => h('div', {
        className: `nav-item${state.filterScope === scope ? ' active' : ''}`,
        onClick: () => { state.filterScope = scope; state.selectedIds.clear(); render(); },
      }, h('span', { className: 'grp-name' }, label), h('span', { className: 'nav-count' }, String(count)));

      const shell = h('div', { className: 'shell' },
        h('header', { className: 'topbar' },
          h('div', { className: 'brand' },
            h('span', { className: 'brand-mark' }, '诗'),
            h('span', null, 'StillPoets 书库管理'),
            h('span', { className: 'sub-pill' }, state.session ? `设备 ${state.session.installId} · rev ${state.session.localRev}` : '未连接 WebDAV')
          ),
          h('div', { className: 'actions' },
            dirty > 0 ? h('button', { className: 'primary', disabled: state.saving, onClick: commitDrafts }, state.saving ? '同步中…' : `保存并同步 (${dirty})`) : null,
            h('button', { disabled: state.loading, onClick: connectAndRefresh }, state.loading ? '拉取中…' : '刷新同步'),
            h('button', { onClick: openCleanupModal }, '清理已删原件'),
            h('button', { onClick: () => { state.modal = 'zlib'; render(); } }, 'Z-Library 域名'),
            h('button', { onClick: () => { state.modal = 'config'; render(); } }, 'WebDAV 设置'),
            h('button', {
              className: 'ghost',
              title: '关闭管理页返回原网页',
              onClick: () => {
                host.remove();
                if (location.hash === MANAGER_HASH) history.replaceState(null, '', location.pathname + location.search);
              },
            }, '✕')
          )
        ),
        h('div', { className: 'workspace' },
          h('aside', { className: 'sidebar' },
            navItem('全部诗集', allBooks.length, 'all'),
            navItem('星标诗集', allBooks.filter((b) => b.isStarred).length, 'starred'),
            navItem('待入库收件箱', inbox.length, 'inbox'),
            h('div', { className: 'nav-section' },
              h('span', null, '自定义分组'),
              h('button', {
                className: 'mini-btn',
                onClick: () => { const n = prompt('输入新分组名称：'); if (n) createGroup(n); },
              }, '+ 新建')
            ),
            groups.map((g, i) => h('div', { className: 'grp-row' },
              navItem(g.name, allBooks.filter((b) => b.groupId === g.id).length, `group:${g.id}`),
              h('div', { className: 'grp-ops' },
                h('button', { className: 'mini-btn ghost', disabled: i === 0, title: '上移', onClick: (e) => { e.stopPropagation(); moveGroupOrder(g.id, -1); } }, '↑'),
                h('button', { className: 'mini-btn ghost', disabled: i === groups.length - 1, title: '下移', onClick: (e) => { e.stopPropagation(); moveGroupOrder(g.id, 1); } }, '↓'),
                h('button', { className: 'mini-btn ghost', title: '重命名', onClick: (e) => { e.stopPropagation(); const next = prompt('重命名分组：', g.name); if (next) renameGroup(g.id, next); } }, '✎'),
                h('button', { className: 'mini-btn ghost', title: '解散', onClick: (e) => { e.stopPropagation(); dissolveGroup(g.id); } }, '×')
              )
            ))
          ),
          h('main', { className: 'main' },
            state.session && state.session.warnings.length > 0 ? h('div', { className: 'warn-banner' }, `同步提醒：${state.session.warnings.join('；')}`) : null,
            dropzone,
            state.uploadLogs.length > 0 ? h('div', { className: 'upload-log' },
              state.uploadLogs.slice(0, 5).map((l) => h('div', null, `${l.ok === true ? '✓' : l.ok === false ? '✗' : '…'} `, h('strong', null, l.name), ` — ${l.status}`))
            ) : null,
            state.filterScope === 'inbox' ? buildInboxCard(inbox, groupNameMap) : buildBooksSection(visibleBooks, groups)
          )
        ),
        buildModal(),
        state.toast ? h('div', { className: `toast ${state.toast.kind}` }, state.toast.msg) : null
      );

      shadow.replaceChildren(styleEl, shell);
    }

    render();
    connectAndRefresh();
  }

  // ============================================================================
  // 5. Z-Library 详情页轻量一键投递按钮（绝不在此页面暴露 WebDAV 凭据输入）
  // ============================================================================

  function isZLibraryHost() {
    const host = location.hostname.toLowerCase();
    return loadZlibHosts().some((dom) => host === dom || host.endsWith('.' + dom));
  }

  function isZLibraryBookPage() {
    return /^\/book\//i.test(location.pathname) || /^\/md5\//i.test(location.pathname);
  }

  function collectNodesDeep(root, acc = [], depth = 0) {
    if (!root || depth > 8) return acc;
    const nodes = root.querySelectorAll ? root.querySelectorAll('*') : [];
    for (let i = 0; i < nodes.length; i++) {
      const el = nodes[i];
      acc.push(el);
      if (el.shadowRoot) {
        collectNodesDeep(el.shadowRoot, acc, depth + 1);
      }
    }
    return acc;
  }

  function resolveCandidateHref(el) {
    if (!el) return '';
    const attrs = ['href', 'data-href', 'data-url', 'data-download', 'data-download-url'];
    for (const attr of attrs) {
      const val = el.getAttribute ? el.getAttribute(attr) : '';
      if (val && val !== '#' && !val.startsWith('javascript:')) {
        try {
          return new URL(val, location.href).href;
        } catch { /* ignore invalid url */ }
      }
    }
    if (el.href && typeof el.href === 'string' && !el.href.startsWith('javascript:')) {
      return el.href;
    }
    return '';
  }

  function findZLibraryEpubDownloadURL() {
    const all = collectNodesDeep(document);

    // 1. 优先匹配明确的下载链接（含 /dl/、addDownloadedBook、dlButton 等）
    const dlCandidates = [];
    for (const el of all) {
      const url = resolveCandidateHref(el);
      if (!url) continue;
      const cls = typeof el.className === 'string' ? el.className : '';
      const txt = (el.textContent || '').trim();
      const fmt = (el.getAttribute && (el.getAttribute('data-format') || el.getAttribute('data-extension') || '')) || '';
      const isDlPath = /\/dl\/|\/download\b|addDownloadedBook|dlButton|download-book/i.test(url + ' ' + cls);
      if (isDlPath) {
        dlCandidates.push({ url, isEpub: /epub/i.test(txt + ' ' + fmt + ' ' + url) });
      }
    }
    const epubDl = dlCandidates.find((c) => c.isEpub);
    if (epubDl) return epubDl.url;
    if (dlCandidates.length > 0) return dlCandidates[0].url;

    // 2. 匹配新版按钮文本（例如 "EPUB, 4.17 MB"），向上/向下查找关联链接
    for (const el of all) {
      const txt = (el.textContent || '').trim();
      if (!txt || txt.length > 60 || !/\bEPUB\b/i.test(txt)) continue;
      let cur = el;
      for (let hop = 0; hop < 4 && cur; hop++) {
        const u = resolveCandidateHref(cur);
        if (u && u !== location.href) return u;
        cur = cur.parentElement || (cur.getRootNode && cur.getRootNode().host) || null;
      }
    }

    // 3. 扫描内联 script 中的 "/dl/<id>/<hash>" 路径（新版异步按钮常把直链放在页面数据里）
    const scripts = document.scripts || [];
    for (let i = 0; i < scripts.length; i++) {
      const code = scripts[i].textContent || '';
      if (!code || !code.includes('/dl/')) continue;
      const m = code.match(/["'](\/dl\/[A-Za-z0-9_-]+\/[A-Za-z0-9_-]+(?:\?[^"']*)?)["']/);
      if (m && m[1]) {
        try {
          return new URL(m[1], location.href).href;
        } catch { /* ignore */ }
      }
    }
    return '';
  }

  function extractPageBookTitle() {
    const all = collectNodesDeep(document);
    const h1 = all.find((el) => el.tagName === 'H1' && (el.textContent || '').trim());
    const raw = (h1 ? h1.textContent : document.title || 'zlibrary-book').trim();
    return raw.replace(/\s+/g, ' ').slice(0, 80) + '.epub';
  }

  function toLightDOMElement(el) {
    let cur = el;
    while (cur) {
      const root = cur.getRootNode ? cur.getRootNode() : null;
      if (!root || root === document) return cur;
      cur = root.host || null;
    }
    return null;
  }

  function findZLibraryActionRow() {
    // 1. 优先匹配 Z-Library 详情页常见按钮容器类名
    const directSelectors = [
      '.book-details-button',
      '.details-buttons-container',
      '.book-actions-container',
      '.bookActionButtons',
      '.details-buttons',
    ];
    for (const sel of directSelectors) {
      const el = document.querySelector(sel);
      if (el && el.offsetParent !== null) return el;
    }

    // 2. 通过“下载/EPUB/Read Online/Add to My Library”按钮反推按钮行容器
    const all = collectNodesDeep(document);
    let hit = null;
    for (const el of all) {
      const url = resolveCandidateHref(el);
      const cls = typeof el.className === 'string' ? el.className : '';
      const txt = (el.textContent || '').trim();
      if (/\/dl\/|addDownloadedBook|dlButton/i.test(url + ' ' + cls) ||
          (txt.length > 0 && txt.length <= 36 && /\bEPUB\b|Read Online|Add to My Library/i.test(txt))) {
        const light = toLightDOMElement(el);
        if (light && light !== document.body && light.offsetParent !== null) {
          hit = light;
          break;
        }
      }
    }
    if (!hit) return null;

    // 向上寻找包含多个动作按钮的水平工具栏（避免退到整页大容器）
    let best = hit;
    let cur = hit.parentElement;
    for (let hop = 0; hop < 5 && cur && cur !== document.body; hop++) {
      const rect = cur.getBoundingClientRect ? cur.getBoundingClientRect() : null;
      if (rect && rect.height >= 34 && rect.height <= 120 && rect.width >= 200) {
        best = cur;
      }
      cur = cur.parentElement;
    }
    return best;
  }

  const ZLIB_WIDGET_CSS = `
  :host { all: initial; font-family: -apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", sans-serif; color: #1C1B19; }
  * { box-sizing: border-box; }
  /* 嵌入式原生操作条（位于 Z-Library 下载按钮正下方） */
  .inline-wrap { display: flex; flex-direction: column; gap: 6px; margin: 12px 0 8px; max-width: 680px; }
  .inline-bar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .btn-send {
    display: inline-flex; align-items: center; gap: 7px; height: 38px; padding: 0 15px; border-radius: 6px;
    background: #BC5B46; color: #FFF; border: 1px solid #BC5B46; cursor: pointer; font-size: 13.5px; font-weight: 500;
    transition: background .15s;
  }
  .btn-send:hover { background: #A44C39; border-color: #A44C39; }
  .btn-send:disabled { opacity: .55; cursor: not-allowed; }
  .badge { display: inline-grid; place-items: center; width: 18px; height: 18px; border-radius: 4px; background: rgba(255,255,255,.22); font-size: 11px; }
  .grp-sel {
    height: 38px; padding: 0 10px; border-radius: 6px; border: 1px solid #D8D8D8; background: #FFF;
    color: #333; font: inherit; font-size: 13px; max-width: 180px;
  }
  .btn-sub {
    height: 38px; padding: 0 12px; border-radius: 6px; border: 1px solid #D8D8D8; background: #FFF;
    color: #444; cursor: pointer; font: inherit; font-size: 13px;
  }
  .btn-sub:hover { background: #F6F6F6; border-color: #C4C4C4; }
  .btn-sub:disabled { opacity: .5; cursor: not-allowed; }
  .extra-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 8px 10px; border-radius: 8px; background: #FAF9F5; border: 1px solid #E6E3DA; }
  .url-in { flex: 1; min-width: 220px; height: 32px; padding: 0 10px; border-radius: 6px; border: 1px solid #D8D3C6; font: inherit; font-size: 12.5px; }
  .st { font-size: 12.5px; color: #5C574E; line-height: 1.45; word-break: break-word; }
  .st.ok { color: #2F6846; }
  .st.err { color: #B4382A; }

  /* 兜底右下角浮钮（仅在未匹配到下载按钮栏时启用） */
  .fab { display: flex; align-items: center; gap: 7px; padding: 10px 15px; border-radius: 99px; background: #BC5B46; color: #FFF; border: none; cursor: pointer; font-size: 13.5px; font-weight: 500; box-shadow: 0 8px 24px rgba(188,91,70,.32); }
  .fab:hover { background: #A44C39; }
  .pop { width: 300px; background: #FFF; border: 1px solid #E6E3DA; border-radius: 12px; padding: 14px; box-shadow: 0 12px 32px rgba(0,0,0,.16); margin-bottom: 10px; font-size: 13px; }
  .pop h4 { margin: 0 0 8px; font-size: 14px; }
  .pop .grp-sel, .pop .url-in { width: 100%; max-width: none; margin-bottom: 8px; }
  .row { display: flex; gap: 8px; margin-top: 4px; }
  .row > button { flex: 1; }
  .sub-link { margin-top: 8px; font-size: 12px; color: #7C776C; display: flex; justify-content: space-between; }
  .link-btn { background: none; border: none; color: #BC5B46; cursor: pointer; padding: 0; font: inherit; text-decoration: underline; }
  `;

  function mountZLibraryQuickButton() {
    if (!isZLibraryHost()) return;
    if (!isZLibraryBookPage() && !findZLibraryEpubDownloadURL()) return;

    const existing = document.getElementById('stillpoets-zlib-btn');
    const anchor = findZLibraryActionRow();
    // 若先前先挂在了右下角兜底，等下载栏异步渲染出来后自动搬迁到下载栏正下方
    if (existing) {
      if (existing.dataset.spMode === 'floating' && anchor && anchor.parentNode) {
        existing.dataset.spMode = 'inline';
        existing.style.cssText = 'display:block;';
        anchor.insertAdjacentElement('afterend', existing);
        if (typeof existing._spRedraw === 'function') existing._spRedraw();
      }
      return;
    }

    const host = document.createElement('div');
    host.id = 'stillpoets-zlib-btn';
    if (anchor && anchor.parentNode) {
      host.dataset.spMode = 'inline';
      host.style.cssText = 'display:block;';
      anchor.insertAdjacentElement('afterend', host);
    } else {
      host.dataset.spMode = 'floating';
      host.style.cssText = 'position:fixed;right:22px;bottom:24px;z-index:2147483645;';
      document.body.appendChild(host);
    }

    const shadow = host.attachShadow({ mode: 'open' });
    const styleEl = document.createElement('style');
    styleEl.textContent = ZLIB_WIDGET_CSS;

    let open = false;
    let busy = false;
    let statusText = '';
    let statusKind = '';
    let manualUrl = '';
    let showExtra = false;

    async function runDeliverFromURL(targetUrl, grpId) {
      const cfg = loadSavedConfig();
      if (!cfg || !cfg.baseURL) {
        statusKind = 'err';
        statusText = '尚未配置 WebDAV，请先点击「打开书库」完成连接配置。';
        draw();
        return;
      }
      GM_setValue(STORAGE_KEYS.lastUploadGroupId, grpId);
      busy = true;
      statusKind = '';
      statusText = '正在复用当前登录态下载 EPUB…';
      draw();
      try {
        // 使用 GM_xmlhttpRequest 跟随 Z-Library 302 跨源 CDN 下载，避免普通 fetch 被 CORS 拦截（不附带任何 WebDAV 凭据）
        const res = await new Promise((resolve, reject) => {
          GM_xmlhttpRequest({
            method: 'GET',
            url: targetUrl,
            headers: { Referer: location.href },
            responseType: 'arraybuffer',
            anonymous: false,
            timeout: 120000,
            onload: resolve,
            onerror: (e) => reject(new Error(e.error || '下载请求失败，请检查网络或油猴跨域权限')),
            ontimeout: () => reject(new Error('下载 EPUB 超时')),
          });
        });
        if (res.status < 200 || res.status >= 300) {
          throw new Error(`站点返回 HTTP ${res.status}`);
        }
        const getHeader = (name) => {
          const m = String(res.responseHeaders || '').match(new RegExp(`^${name}\\s*:\\s*(.+)$`, 'im'));
          return m ? m[1].trim() : '';
        };
        const ct = getHeader('content-type').toLowerCase();
        const bytes = new Uint8Array(res.response || new ArrayBuffer(0));
        if (ct.includes('text/html') || !isZipMagic(bytes)) {
          throw new Error('返回内容不是有效的 EPUB（可能是限额提示、需先登录或跳转页），可展开「更多」手动选文件投递');
        }
        let filename = extractPageBookTitle();
        const cd = getHeader('content-disposition');
        const fnMatch = cd.match(/filename\*=UTF-8''([^;]+)|filename="?([^";]+)"?/i);
        if (fnMatch) filename = decodePathSafe(fnMatch[1] || fnMatch[2] || filename);
        const client = new WebDAVClient(cfg);
        const session = new StillPoetsSyncSession(client);
        await session.deliverEPUB(bytes, filename, grpId, 'zlibrary', (msg) => {
          statusText = msg;
          draw();
        });
        statusKind = 'ok';
        statusText = '✓ 已投递到 WebDAV 收件箱！打开手机或平板同步即自动解析入库。';
      } catch (err) {
        statusKind = 'err';
        statusText = `✗ ${err.message}`;
      } finally {
        busy = false;
        draw();
      }
    }

    async function runDeliverFromFile(file, grpId) {
      const cfg = loadSavedConfig();
      if (!cfg || !cfg.baseURL) {
        statusKind = 'err';
        statusText = '尚未配置 WebDAV，请先点击「打开书库」完成连接配置。';
        draw();
        return;
      }
      GM_setValue(STORAGE_KEYS.lastUploadGroupId, grpId);
      busy = true;
      statusKind = '';
      statusText = `正在读取 ${file.name}…`;
      draw();
      try {
        const bytes = new Uint8Array(await file.arrayBuffer());
        const client = new WebDAVClient(cfg);
        const session = new StillPoetsSyncSession(client);
        await session.deliverEPUB(bytes, file.name, grpId, 'zlibrary', (msg) => {
          statusText = msg;
          draw();
        });
        statusKind = 'ok';
        statusText = '✓ 已投递到 WebDAV 收件箱！打开手机或平板同步即自动解析入库。';
      } catch (err) {
        statusKind = 'err';
        statusText = `✗ ${err.message}`;
      } finally {
        busy = false;
        draw();
      }
    }

    function draw() {
      const isInline = host.dataset.spMode === 'inline';
      const cachedGroups = GM_getValue(STORAGE_KEYS.cachedGroups, []);
      const lastGroup = GM_getValue(STORAGE_KEYS.lastUploadGroupId, '');
      const grpSelect = h('select', { className: 'grp-sel', disabled: busy, title: '选择入库分组' },
        h('option', { value: '', selected: !lastGroup }, '分组：全部诗集'),
        cachedGroups.map((g) => h('option', { value: g.id, selected: g.id === lastGroup }, g.name))
      );
      const localFileInput = h('input', {
        type: 'file',
        accept: '.epub',
        style: 'display:none',
        onChange: (e) => {
          const f = e.target.files && e.target.files[0];
          e.target.value = '';
          if (f) runDeliverFromFile(f, grpSelect.value);
        },
      });
      const triggerDeliver = () => {
        const detected = manualUrl.trim() || findZLibraryEpubDownloadURL();
        if (!detected) {
          showExtra = true;
          statusKind = 'err';
          statusText = '未自动识别到直链。可右键「EPUB」按钮复制链接贴到下方，或本地选 EPUB 投递。';
          draw();
          return;
        }
        runDeliverFromURL(detected, grpSelect.value);
      };

      if (isInline) {
        const extraBox = showExtra ? h('div', { className: 'extra-row' },
          h('input', {
            className: 'url-in',
            type: 'text',
            placeholder: '可选：粘贴右键复制的 EPUB 下载链接…',
            value: manualUrl,
            disabled: busy,
            onInput: (e) => { manualUrl = e.target.value; },
          }),
          h('button', { className: 'btn-sub', style: 'height:32px', disabled: busy, onClick: () => localFileInput.click() }, '选本地 EPUB')
        ) : null;

        const bar = h('div', { className: 'inline-wrap' },
          h('div', { className: 'inline-bar' },
            h('button', { className: 'btn-send', disabled: busy, onClick: triggerDeliver },
              h('span', { className: 'badge' }, '诗'),
              busy ? '投递中…' : '发送到 StillPoets'
            ),
            grpSelect,
            h('button', { className: 'btn-sub', onClick: () => GM_openInTab(MANAGER_ENTRY_URL, { active: true }) }, '打开书库'),
            h('button', { className: 'btn-sub', onClick: () => { showExtra = !showExtra; draw(); } }, showExtra ? '收起' : '更多')
          ),
          localFileInput,
          extraBox,
          statusText ? h('div', { className: `st ${statusKind}` }, statusText) : null
        );
        shadow.replaceChildren(styleEl, bar);
        return;
      }

      // 兜底右下角浮钮模式
      const urlInput = showExtra ? h('input', {
        className: 'url-in',
        type: 'text',
        placeholder: '粘贴右键复制的 EPUB 下载链接…',
        value: manualUrl,
        disabled: busy,
        onInput: (e) => { manualUrl = e.target.value; },
      }) : null;
      const pop = open ? h('div', { className: 'pop' },
        h('h4', null, '投递到 StillPoets'),
        grpSelect,
        urlInput,
        h('div', { className: 'row' },
          h('button', { className: 'btn-sub', onClick: () => GM_openInTab(MANAGER_ENTRY_URL, { active: true }) }, '打开书库'),
          h('button', { className: 'btn-send', disabled: busy, onClick: triggerDeliver }, busy ? '投递中…' : '立即投递')
        ),
        localFileInput,
        h('div', { className: 'sub-link' },
          h('button', { className: 'link-btn', disabled: busy, onClick: () => localFileInput.click() }, '本地选 EPUB 投递'),
          h('button', { className: 'link-btn', disabled: busy, onClick: () => { showExtra = !showExtra; draw(); } }, showExtra ? '收起链接框' : '手动贴下载链接')
        ),
        statusText ? h('div', { className: `st ${statusKind}` }, statusText) : null
      ) : null;
      const fab = h('button', { className: 'fab', onClick: () => { open = !open; draw(); } }, '诗 · 发送到 StillPoets');
      shadow.replaceChildren(styleEl, h('div', null, pop, fab));
    }

    host._spRedraw = draw;
    draw();
  }

  // 监听 SPA 路由与异步加载的 Z-Library 详情页
  function watchZLibraryPage() {
    if (!isZLibraryHost()) return;
    mountZLibraryQuickButton();
    let timer = null;
    const obs = new MutationObserver(() => {
      const existing = document.getElementById('stillpoets-zlib-btn');
      if (existing && existing.dataset.spMode === 'inline') return;
      clearTimeout(timer);
      timer = setTimeout(mountZLibraryQuickButton, 300);
    });
    if (document.body) {
      obs.observe(document.body, { childList: true, subtree: true });
    }
  }
  // ============================================================================
  // 6. 入口注册
  // ============================================================================

  if (typeof GM_registerMenuCommand === 'function') {
    GM_registerMenuCommand('📚 打开 StillPoets 书库管理', () => {
      if (isManagerEntryPage()) {
        mountManagerApp();
      } else {
        GM_openInTab(MANAGER_ENTRY_URL, { active: true });
      }
    });
  }

  if (isManagerEntryPage()) {
    mountManagerApp();
  } else {
    watchZLibraryPage();
  }

  window.addEventListener('hashchange', () => {
    if (isManagerEntryPage()) mountManagerApp();
  });
})();
