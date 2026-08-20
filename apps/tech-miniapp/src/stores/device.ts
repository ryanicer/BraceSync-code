import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Device } from '@bracesync/shared-types'

export const useDeviceStore = defineStore('device', () => {
  const currentDevice = ref<Device | null>(null)
  const isBound = ref(false)
  const bleConnected = ref(false)

  function setDevice(device: Device) {
    currentDevice.value = device
    isBound.value = device.status !== 'unbound'
  }

  function clearDevice() {
    currentDevice.value = null
    isBound.value = false
    bleConnected.value = false
  }

  function setBleConnected(val: boolean) {
    bleConnected.value = val
  }

  return { currentDevice, isBound, bleConnected, setDevice, clearDevice, setBleConnected }
})
