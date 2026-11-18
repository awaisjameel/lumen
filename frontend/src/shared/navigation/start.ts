import { START_VIEWS } from '../../bridge'
import type { StartDestinationName } from './contract'

/** Keep the generated preference vocabulary checked against eligible destinations. */
export const startViews = START_VIEWS satisfies readonly StartDestinationName[]
