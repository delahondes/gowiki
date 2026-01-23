import { initPMPlayground } from "./playground_pm"

declare global {
  interface Window {
    initPMPlayground?: (container: HTMLElement) => void
  }
}

window.initPMPlayground = initPMPlayground