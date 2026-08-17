/**
 * Brand Film revisions created before a field was introduced may contain
 * `null` instead of an array. Normalize at the display boundary so a single
 * optional value cannot take down the entire workspace.
 */
export const stringListOrEmpty = (value: unknown): string[] => Array.isArray(value)
  ? value.filter((item): item is string => typeof item === 'string')
  : []
