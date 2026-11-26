import {useCallback, useEffect, useState} from 'react'
import {api} from '../api'

export type ThemePref = 'light' | 'dark' | 'system'

function apply(pref: ThemePref) {
  const dark = pref === 'dark' || (pref === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
}

export function useTheme() {
  const [pref, setPref] = useState<ThemePref>(() => {
    try { return (localStorage.getItem('theme') as ThemePref) || 'system' } catch { return 'system' }
  })

  useEffect(() => {
    api.getSettings().then(s => {
      if (s.theme === 'light' || s.theme === 'dark' || s.theme === 'system') setPref(s.theme)
    }).catch(() => {})
  }, [])

  useEffect(() => {
    apply(pref)
    if (pref !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const h = () => apply('system')
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [pref])

  const set = useCallback((p: ThemePref) => {
    setPref(p)
    try { localStorage.setItem('theme', p) } catch { /* ignorar */ }
    api.setSetting('theme', p).catch(() => {})
  }, [])
  return {pref, set}
}
