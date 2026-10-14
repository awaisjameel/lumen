import assert from 'node:assert/strict'
import test from 'node:test'
import { checkBlockedThumbnail } from './thumbs-privacy.mjs'

const refusal = () => ({
  id: 17,
  after: {
    normal: { status: 404 },
    forceCache: { status: 404 },
    onlyIfCached: { status: 504 },
    image: { loaded: false },
  },
  gridAfterReturn: { decodedIDs: [18, 19] },
})

test('Refused URLs and a grid without the blocked image pass the privacy probe.', () => {
  assert.doesNotThrow(() => checkBlockedThumbnail(refusal()))
})

test('Every successful URL access fails the privacy probe.', () => {
  for (const access of ['normal', 'forceCache', 'onlyIfCached']) {
    const probe = refusal()
    probe.after[access].status = 200
    assert.throws(() => checkBlockedThumbnail(probe), /still reads/)
  }
  const probe = refusal()
  probe.after.image.loaded = true
  assert.throws(() => checkBlockedThumbnail(probe), /new image/)
})

test('A blocked image painted again fails even when every URL is refused.', () => {
  const probe = refusal()
  probe.gridAfterReturn.decodedIDs.push(probe.id)
  assert.throws(() => checkBlockedThumbnail(probe), /grid painted/)
})
