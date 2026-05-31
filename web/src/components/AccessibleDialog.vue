<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  open: boolean
  titleId: string
  descriptionId?: string
  panelClass?: string
  closeOnBackdrop?: boolean
}>(), {
  descriptionId: undefined,
  panelClass: '',
  closeOnBackdrop: true
})

const emit = defineEmits<{ close: [] }>()

const dialogEl = ref<HTMLElement | null>(null)
let previousFocus: HTMLElement | null = null

function dialogFocusableElements() {
  if (!dialogEl.value) {
    return []
  }
  const selector = [
    'a[href]',
    'button:not([disabled])',
    'input:not([disabled]):not([type="hidden"])',
    'select:not([disabled])',
    'textarea:not([disabled])',
    '[tabindex]:not([tabindex="-1"])',
    '[contenteditable="true"]'
  ].join(',')
  return Array.from(dialogEl.value.querySelectorAll<HTMLElement>(selector)).filter(
    (element) =>
      !element.hasAttribute('aria-hidden') &&
      !element.hasAttribute('inert') &&
      element.getAttribute('data-disabled') !== 'true' &&
      element.offsetParent !== null
  )
}

async function focusDialog() {
  await nextTick()
  const focusable = dialogFocusableElements()
  if (focusable.length > 0) {
    focusable[0].focus()
    return
  }
  dialogEl.value?.focus()
}

function handleBackdropMouseDown(event: MouseEvent) {
  if (!props.closeOnBackdrop) {
    return
  }
  if (event.target === event.currentTarget) {
    emit('close')
  }
}

function handleKeyDown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    emit('close')
    return
  }
  if (event.key !== 'Tab') {
    return
  }
  const focusable = dialogFocusableElements()
  if (focusable.length === 0) {
    event.preventDefault()
    dialogEl.value?.focus()
    return
  }
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  const active = document.activeElement as HTMLElement | null
  if (event.shiftKey && active === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && active === last) {
    event.preventDefault()
    first.focus()
  }
}

watch(
  () => props.open,
  async (isOpen) => {
    if (isOpen) {
      previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
      await focusDialog()
      return
    }
    if (previousFocus && document.contains(previousFocus)) {
      previousFocus.focus()
    }
    previousFocus = null
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  if (previousFocus && document.contains(previousFocus)) {
    previousFocus.focus()
  }
})
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="modal-backdrop" @mousedown="handleBackdropMouseDown">
      <div
        ref="dialogEl"
        :class="['modal-panel', panelClass]"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="titleId"
        :aria-describedby="descriptionId"
        tabindex="-1"
        @keydown="handleKeyDown"
      >
        <slot />
      </div>
    </div>
  </Teleport>
</template>
