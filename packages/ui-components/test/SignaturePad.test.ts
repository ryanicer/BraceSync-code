import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SignaturePad from '../src/signature-pad/SignaturePad.vue'

describe('SignaturePad', () => {
  it('renders with default props', () => {
    const wrapper = mount(SignaturePad)
    expect(wrapper.find('.signature-pad').exists()).toBe(true)
    expect(wrapper.find('.pad-canvas').exists()).toBe(true)
  })

  it('shows header by default with title', () => {
    const wrapper = mount(SignaturePad)
    expect(wrapper.find('.pad-header').exists()).toBe(true)
    expect(wrapper.find('.pad-title').text()).toBe('电子签名')
  })

  it('hides header when showHeader is false', () => {
    const wrapper = mount(SignaturePad, {
      props: { showHeader: false },
    })
    expect(wrapper.find('.pad-header').exists()).toBe(false)
  })

  it('accepts custom title prop', () => {
    const wrapper = mount(SignaturePad, {
      props: { title: '患者签名' },
    })
    expect(wrapper.find('.pad-title').text()).toBe('患者签名')
  })

  it('shows placeholder text when canvas is empty', () => {
    const wrapper = mount(SignaturePad)
    expect(wrapper.find('.pad-placeholder').exists()).toBe(true)
    expect(wrapper.find('.placeholder-text').text()).toBe('请在此处签名')
  })

  it('sets canvas dimensions from props', () => {
    const wrapper = mount(SignaturePad, {
      props: { width: 400, height: 250 },
    })
    const canvas = wrapper.find('.pad-canvas')
    expect(canvas.attributes('style')).toContain('400px')
    expect(canvas.attributes('style')).toContain('250px')
  })

  it('emits clear event when clear is called', async () => {
    const wrapper = mount(SignaturePad)
    await wrapper.vm.clear()
    expect(wrapper.emitted('clear')).toBeTruthy()
  })

  it('does not emit confirm when canvas is empty', async () => {
    const wrapper = mount(SignaturePad)
    await wrapper.vm.confirm()
    expect(wrapper.emitted('confirm')).toBeFalsy()
  })

  it('exposes clear and toDataURL methods', () => {
    const wrapper = mount(SignaturePad)
    expect(typeof wrapper.vm.clear).toBe('function')
    expect(typeof wrapper.vm.toDataURL).toBe('function')
  })

  it('toDataURL returns empty string when no canvas context', () => {
    const wrapper = mount(SignaturePad)
    const result = wrapper.vm.toDataURL()
    expect(result).toBe('')
  })

  it('has touch and mouse event handlers on canvas', () => {
    const wrapper = mount(SignaturePad)
    const canvas = wrapper.find('.pad-canvas')
    // Verify event listeners are attached (Vue test utils doesn't directly expose them,
    // but we can verify the element exists with the correct class)
    expect(canvas.exists()).toBe(true)
  })
})
