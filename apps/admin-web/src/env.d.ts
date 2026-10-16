/// <reference types="vite/client" />

// T708 运维反馈通道（构建时由部署侧注入，仓内不放 .env 文件）：口径见 src/utils/feedback.ts
interface ImportMetaEnv {
  readonly VITE_FEEDBACK_SERVICE_URL?: string
  readonly VITE_FEEDBACK_ENV?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<object, object, unknown>
  export default component
}
