import { ref, onMounted, onBeforeUnmount, watch, type Ref } from 'vue'

/**
 * 响应式断点检测（三档）：
 *  - ≥1280px：desktop（完整布局）
 *  - 768-1279px：tablet（侧边栏可折叠）
 *  - <768px：mobile（侧边栏 drawer）
 */
export type Breakpoint = 'desktop' | 'tablet' | 'mobile'

const DESKTOP_MQ = '(min-width: 1280px)'
const TABLET_MQ = '(min-width: 768px) and (max-width: 1279px)'
const MOBILE_MQ = '(max-width: 767px)'

export function useResponsive() {
  const breakpoint = ref<Breakpoint>('desktop')
  const isDesktop = ref(true)
  const isTablet = ref(false)
  const isMobile = ref(false)

  function update() {
    if (window.matchMedia(DESKTOP_MQ).matches) {
      breakpoint.value = 'desktop'
      isDesktop.value = true
      isTablet.value = false
      isMobile.value = false
    } else if (window.matchMedia(TABLET_MQ).matches) {
      breakpoint.value = 'tablet'
      isDesktop.value = false
      isTablet.value = true
      isMobile.value = false
    } else {
      breakpoint.value = 'mobile'
      isDesktop.value = false
      isTablet.value = false
      isMobile.value = true
    }
  }

  let mqlDesktop: MediaQueryList | null = null
  let mqlTablet: MediaQueryList | null = null
  let mqlMobile: MediaQueryList | null = null

  onMounted(() => {
    update()
    mqlDesktop = window.matchMedia(DESKTOP_MQ)
    mqlTablet = window.matchMedia(TABLET_MQ)
    mqlMobile = window.matchMedia(MOBILE_MQ)
    mqlDesktop.addEventListener('change', update)
    mqlTablet.addEventListener('change', update)
    mqlMobile.addEventListener('change', update)
  })

  onBeforeUnmount(() => {
    mqlDesktop?.removeEventListener('change', update)
    mqlTablet?.removeEventListener('change', update)
    mqlMobile?.removeEventListener('change', update)
  })

  return { breakpoint, isDesktop, isTablet, isMobile }
}

/**
 * localStorage 持久化的布尔 ref。
 * 返回一个可写 ref，修改时自动同步到 localStorage。
 * 模板中可直接用于 v-model。
 */
export function useLocalStorageBool(key: string, defaultValue: boolean): Ref<boolean> {
  const readStored = (): boolean => {
    if (typeof window === 'undefined') return defaultValue
    const stored = localStorage.getItem(key)
    return stored !== null ? stored === 'true' : defaultValue
  }

  const value = ref(readStored()) as Ref<boolean>

  watch(value, (newVal) => {
    if (typeof window !== 'undefined') {
      localStorage.setItem(key, String(newVal))
    }
  })

  return value
}
