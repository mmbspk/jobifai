import type { ApplicationDefaults } from '../types'

// Merge imported/reviewed CV facts with independently saved application answers.
// Competing edits remain visible and require a candidate decision before saving.
export function mergeApplicationFactDraft(base: ApplicationDefaults = {}, draft: ApplicationDefaults = {}, saved: ApplicationDefaults = {}) {
  const value: ApplicationDefaults = { ...saved }
  const conflicts: string[] = []
  for (const key of new Set([...Object.keys(base), ...Object.keys(draft), ...Object.keys(saved)]) as Set<keyof ApplicationDefaults>) {
    const localChanged = JSON.stringify(draft[key]) !== JSON.stringify(base[key])
    if (!localChanged) continue
    const remoteChanged = JSON.stringify(saved[key]) !== JSON.stringify(base[key])
    if (remoteChanged && JSON.stringify(saved[key]) !== JSON.stringify(draft[key])) conflicts.push(key)
    Object.assign(value, { [key]: draft[key] })
  }
  return { value, conflicts }
}
