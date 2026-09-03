import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { InstallRecord, Baseline } from '@bracesync/shared-types'

export const useInstallStore = defineStore('install', () => {
  const currentInstall = ref<Partial<InstallRecord> | null>(null)
  const currentBaseline = ref<Partial<Baseline> | null>(null)
  const matrixStep = ref(0)

  function startInstall(deviceId: string, patientId: string, techId: string) {
    currentInstall.value = {
      deviceId,
      patientId,
      techId,
      calibrateTime: new Date().toISOString(),
      notes: '',
      signatureUrl: '',
      wifiStatus: 'unconfigured',
    }
    matrixStep.value = 0
  }

  function setMatrixStep(step: number) {
    matrixStep.value = step
  }

  function setBaseline(offsetValues: number[]) {
    currentBaseline.value = {
      deviceId: currentInstall.value?.deviceId || '',
      offsetValues,
      calibratorId: currentInstall.value?.techId || '',
      createdAt: new Date().toISOString(),
    }
  }

  function setSignature(url: string) {
    if (currentInstall.value) {
      currentInstall.value.signatureUrl = url
    }
  }

  function setNotes(notes: string) {
    if (currentInstall.value) {
      currentInstall.value.notes = notes
    }
  }

  function completeInstall() {
    if (currentInstall.value) {
      currentInstall.value.wifiStatus = 'connected'
    }
  }

  function resetInstall() {
    currentInstall.value = null
    currentBaseline.value = null
    matrixStep.value = 0
  }

  return {
    currentInstall, currentBaseline, matrixStep,
    startInstall, setMatrixStep, setBaseline, setSignature, setNotes, completeInstall, resetInstall,
  }
})
