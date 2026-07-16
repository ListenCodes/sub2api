import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useDebouncedAction } from '../useDebouncedAction'

describe('useDebouncedAction', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('runs once 300 ms after the latest scheduled call', async () => {
    const action = vi.fn()
    const { schedule } = useDebouncedAction(action, 300)

    schedule()
    await vi.advanceTimersByTimeAsync(200)
    schedule()
    await vi.advanceTimersByTimeAsync(299)
    expect(action).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(action).toHaveBeenCalledTimes(1)
  })

  it('flushes immediately and cancels pending work', async () => {
    const action = vi.fn()
    const { schedule, runNow } = useDebouncedAction(action, 300)

    schedule()
    await runNow()
    expect(action).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(300)
    expect(action).toHaveBeenCalledTimes(1)
  })

  it('supports explicit cancellation', async () => {
    const action = vi.fn()
    const { schedule, cancel } = useDebouncedAction(action, 300)

    schedule()
    cancel()
    await vi.advanceTimersByTimeAsync(300)
    expect(action).not.toHaveBeenCalled()
  })

  it('cancels pending work when the owner unmounts', async () => {
    const action = vi.fn()
    const owner = mount(defineComponent({
      setup() {
        const controls = useDebouncedAction(action, 300)
        controls.schedule()
        return () => null
      }
    }))

    owner.unmount()
    await vi.advanceTimersByTimeAsync(300)
    expect(action).not.toHaveBeenCalled()
  })
})
