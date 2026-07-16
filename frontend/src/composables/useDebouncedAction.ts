import { getCurrentInstance, onBeforeUnmount } from 'vue'

export function useDebouncedAction(action: () => void | Promise<void>, delay = 300) {
  let timer: number | undefined

  const cancel = () => {
    if (timer !== undefined) {
      window.clearTimeout(timer)
      timer = undefined
    }
  }

  const runNow = () => {
    cancel()
    return action()
  }

  const schedule = () => {
    cancel()
    timer = window.setTimeout(() => {
      timer = undefined
      void action()
    }, delay)
  }

  if (getCurrentInstance()) {
    onBeforeUnmount(cancel)
  }

  return { schedule, runNow, cancel }
}
