<script setup lang="ts">
import { ref } from 'vue';
import { api } from '../api';
import { saveDownload } from '../saveDownload';
const props = withDefaults(defineProps<{ path: string; label?: string; disabled?: boolean; description?: string }>(), { label: '导出 Excel', description: '按当前筛选条件导出全部记录，不限当前页' });
const emit = defineEmits<{ success: [message: string]; error: [message: string] }>();
const busy = ref(false);
async function download() {
  if (busy.value || props.disabled) return;
  busy.value = true;
  try { emit('success', await saveDownload(await api.download(props.path))); }
  catch (err) { emit('error', err instanceof Error ? err.message : '导出失败，请重试'); }
  finally { busy.value = false; }
}
</script>
<template>
  <button type="button" class="secondary-button export-button" :title="description" :disabled="disabled || busy" :aria-busy="busy" @click="download">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="M12 3v12m-4-4 4 4 4-4M4 16v4h16v-4" /></svg>
    {{ busy ? '正在导出…' : label }}
  </button>
</template>
