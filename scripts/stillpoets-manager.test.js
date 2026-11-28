const test = require('node:test');
const assert = require('node:assert/strict');
const core = require('./stillpoets-manager.user.js');

test('parseSnapshotFilename and parseInboxReadyFilename validation', () => {
  assert.deepEqual(core.parseSnapshotFilename('device_tm-12ab_3.json.gz'), { installId: 'tm-12ab', rev: 3 });
  assert.equal(core.parseSnapshotFilename('device_tm-12ab_03.json.gz'), null);
  assert.equal(core.parseSnapshotFilename('device_tm-12ab_0.json.gz'), null);
  assert.equal(core.parseSnapshotFilename('device_bad_id_1.json.gz'), null);

  assert.equal(core.parseInboxReadyFilename('sp-abc-123.ready.json'), 'sp-abc-123');
  assert.equal(core.parseInboxReadyFilename('..ready.json'), null);
  assert.equal(core.parseInboxReadyFilename('...ready.json'), null);
});

test('mergeBooks preserves ContentPath and independent field versions', () => {
  const mobile = [{
    id: 'b1',
    title: '海子诗全集',
    author: '海子',
    contentHash: 'h1',
    contentSize: 200,
    contentPath: 'builds/att1/parsed.spbook.json.gz',
    rawFilename: 'haizi_1.epub',
    poemCount: 12,
    contentVer: { c: 2, i: 'ios-1' },
    groupId: 'g1',
    groupVer: { c: 2, i: 'ios-1' },
    isStarred: false,
    starredVer: { c: 2, i: 'ios-1' },
    sortOrder: 0,
    sortVer: { c: 2, i: 'ios-1' },
    createdAt: 1000,
  }];
  const web = [{
    ...mobile[0],
    groupId: 'g2',
    groupVer: { c: 5, i: 'tm-1' },
    isStarred: true,
    starredVer: { c: 5, i: 'tm-1' },
  }];
  const merged = core.mergeBooks(mobile, web);
  assert.equal(merged.length, 1);
  assert.equal(merged[0].groupId, 'g2');
  assert.equal(merged[0].isStarred, true);
  assert.equal(merged[0].contentPath, 'builds/att1/parsed.spbook.json.gz');
});

test('buildCumulativeSparseSnapshot retains previous managed records and omits other collections', () => {
  const prevOwn = {
    groups: [
      { id: 'g1', name: '现代诗', nameVer: { c: 3, i: 'tm-1' }, sortOrder: 0, sortVer: { c: 3, i: 'tm-1' }, createdAt: 100 },
    ],
    books: [
      {
        id: 'b-old', title: '旧诗集', author: '甲', contentHash: 'h-old', contentSize: 10,
        rawFilename: 'old.epub', poemCount: 1, contentVer: { c: 1, i: 'ios-1' },
        isStarred: true, starredVer: { c: 4, i: 'tm-1' }, sortOrder: 0, createdAt: 50,
      },
    ],
  };
  const snap = core.buildCumulativeSparseSnapshot({
    installId: 'tm-1',
    nextRev: 2,
    maxCounter: 6,
    previousOwnData: prevOwn,
    mutatedGroups: [
      { id: 'g2', name: '外国诗', nameVer: { c: 7, i: 'tm-1' }, sortOrder: 1, sortVer: { c: 7, i: 'tm-1' }, createdAt: 200 },
    ],
    mutatedBooks: [
      {
        id: 'b-new', title: '新诗集', author: '乙', contentHash: 'h-new', contentSize: 20,
        rawFilename: 'new.epub', poemCount: 3, contentVer: { c: 5, i: 'ios-1' },
        groupId: 'g2', groupVer: { c: 7, i: 'tm-1' }, isStarred: false, sortOrder: 0, createdAt: 150,
      },
    ],
    nowMs: 1700000000000,
  });

  assert.equal(snap.schemaVersion, 2);
  assert.equal(snap.rev, 2);
  assert.equal(snap.maxCounter, 7);
  assert.equal(snap.data.groups.length, 2);
  assert.equal(snap.data.books.length, 2);
  assert.equal('favorites' in snap.data, false);
  assert.equal('quotes' in snap.data, false);
  assert.equal('progress' in snap.data, false);
  assert.equal('readingEvents' in snap.data, false);
});

test('validateDecodedSnapshot detects unsafe 64-bit counter and switches to read-only', () => {
  const checked = core.validateDecodedSnapshot({
    schemaVersion: 2,
    installId: 'ios-1',
    rev: 1,
    maxCounter: Number.MAX_SAFE_INTEGER + 10,
    generatedAt: 1700000000000,
    data: {},
  }, 'ios-1', 1);
  assert.equal(checked.unsafeInt, true);
});

test('isZipMagic and buildSafeRawFilename', () => {
  assert.equal(core.isZipMagic(new Uint8Array([0x50, 0x4b, 0x03, 0x04])), true);
  assert.equal(core.isZipMagic(new Uint8Array([0x3c, 0x68, 0x74, 0x6d])), false);
  assert.equal(core.buildSafeRawFilename('../海子:诗选.epub', 'abc123'), '海子_诗选_abc123.epub');
});
