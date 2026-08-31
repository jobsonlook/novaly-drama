import { inject } from 'vue'
import { NovalyKey, type NovalyContext } from './useNovaly'

export function useNovalyInject(): NovalyContext {
  const ctx = inject(NovalyKey)
  if (!ctx) throw new Error('Novaly context not provided')
  return ctx
}
