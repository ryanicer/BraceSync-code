import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusIndicator from '../src/status-indicator/StatusIndicator.vue'

describe('StatusIndicator', () => {
  it('renders with online status', () => {
    const wrapper = mount(StatusIndicator, {
      props: { status: 'online', label: '设备在线' },
    })
    expect(wrapper.find('.status-online').exists()).toBe(true)
    expect(wrapper.text()).toContain('设备在线')
  })

  it('renders with offline status', () => {
    const wrapper = mount(StatusIndicator, {
      props: { status: 'offline', label: '设备离线' },
    })
    expect(wrapper.find('.status-offline').exists()).toBe(true)
    expect(wrapper.text()).toContain('设备离线')
  })

  it('renders with warning status', () => {
    const wrapper = mount(StatusIndicator, {
      props: { status: 'warning', label: '告警' },
    })
    expect(wrapper.find('.status-warning').exists()).toBe(true)
    expect(wrapper.text()).toContain('告警')
  })

  it('renders with loading status', () => {
    const wrapper = mount(StatusIndicator, {
      props: { status: 'loading', label: '加载中' },
    })
    expect(wrapper.find('.status-loading').exists()).toBe(true)
    expect(wrapper.text()).toContain('加载中')
  })

  it('applies correct CSS class based on status prop', () => {
    const wrapper = mount(StatusIndicator, {
      props: { status: 'online', label: 'test' },
    })
    expect(wrapper.classes()).toContain('status-indicator')
    expect(wrapper.classes()).toContain('status-online')
  })
})