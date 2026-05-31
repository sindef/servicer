<script setup lang="ts">
import { onMounted, onUnmounted, watch, ref } from 'vue'
import { EditorView, basicSetup } from 'codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { EditorState } from '@codemirror/state'

const props = withDefaults(defineProps<{ modelValue: string; ariaLabel?: string }>(), {
  ariaLabel: 'YAML editor'
})
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const container = ref<HTMLElement | null>(null)
let view: EditorView | null = null

const highContrastTheme = EditorView.theme({
  '&': {
    backgroundColor: '#ffffff',
    color: '#111827'
  },
  '.cm-content': {
    caretColor: '#111827'
  },
  '.cm-cursor, .cm-dropCursor': {
    borderLeftColor: '#111827'
  },
  '.cm-gutters': {
    backgroundColor: '#f3f4f6',
    color: '#374151',
    border: 'none'
  },
  '.cm-activeLine': {
    backgroundColor: '#eff6ff'
  },
  '.cm-activeLineGutter': {
    backgroundColor: '#dbeafe'
  }
})

onMounted(() => {
  view = new EditorView({
    state: EditorState.create({
      doc: props.modelValue,
      extensions: [
        basicSetup,
        yaml(),
        highContrastTheme,
        EditorView.contentAttributes.of({
          'aria-label': props.ariaLabel,
          role: 'textbox',
          'aria-multiline': 'true'
        }),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) emit('update:modelValue', update.state.doc.toString())
        }),
      ],
    }),
    parent: container.value!,
  })
})

watch(() => props.modelValue, (newVal) => {
  if (view && view.state.doc.toString() !== newVal) {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: newVal } })
  }
})

onUnmounted(() => { view?.destroy() })
</script>

<template>
  <div ref="container" class="yaml-editor" role="region" :aria-label="props.ariaLabel" />
</template>
