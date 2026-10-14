export function checkBlockedThumbnail(probe) {
  for (const access of ['normal', 'forceCache', 'onlyIfCached']) {
    const response = probe.after[access]
    if (response.status < 400) throw new Error(`${access} still reads a blocked thumbnail: ${response.status}`)
  }
  if (probe.after.image.loaded) throw new Error('a new image still reads the blocked thumbnail')
  if (probe.gridAfterReturn.decodedIDs.includes(probe.id))
    throw new Error('the grid painted the previously shown thumbnail after its block')
}
