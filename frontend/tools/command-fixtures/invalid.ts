import { commandById, registerCommands, runCommand } from '../../src/shared/commands'
import { duplicateCommand, itemCommand } from '../../src/shared/commandContract'
import { DuplicateKind } from '../../src/bridge'
// expect-rejection
runCommand('media.favourtie')
// expect-rejection
commandById('person.not-a-number.rename')
// expect-rejection
runCommand('person.12.rename')
// expect-rejection
registerCommands([{ id: 'unknown.action', run: () => undefined }])
// expect-rejection
itemCommand('person.name', 12)
// expect-rejection
itemCommand('person.rename', '12')
// expect-rejection
runCommand({ name: 'person.rename', id: '12' })
// expect-rejection
runCommand({ name: 'person.rename', id: 12, kind: DuplicateKind.DuplicateExact })
// expect-rejection
runCommand({ name: 'duplicates.review', id: 12, kind: 'unknown' })
// expect-rejection
registerCommands([{ id: itemCommand('person.rename', 12), run: () => undefined }])
// expect-rejection
registerCommands([{ id: 'media.open', keys: 'F2', run: () => undefined }])
// expect-rejection
commandById('media.open')?.run()
// expect-rejection
registerCommands([{ id: 'media.open', zone: 'grid', run: () => undefined }])
// expect-rejection
registerCommands([{ id: duplicateCommand(DuplicateKind.DuplicateExact, 1), run: () => undefined }])
