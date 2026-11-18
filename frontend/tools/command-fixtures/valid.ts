import { commandById, registerCommands, runCommand } from '../../src/shared/commands'
import { duplicateCommand, itemCommand } from '../../src/shared/commandContract'
import { DuplicateKind } from '../../src/bridge'
void runCommand('media.open')
void runCommand(itemCommand('person.rename', 12))
void runCommand(duplicateCommand(DuplicateKind.DuplicateExact, 12))
commandById('media.open')?.title()
registerCommands([{ id: 'media.open', available: () => true, run: () => undefined }])
registerCommands([{ id: itemCommand('person.rename', 12), title: () => 'Rename', run: () => undefined }])
