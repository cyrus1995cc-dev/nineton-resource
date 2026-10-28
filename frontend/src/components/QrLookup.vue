<script setup lang="ts">
import { ref } from 'vue';
import QrScanner from './QrScanner.vue';
import { getAssetCodeFromScan } from '../qrLookup';

const emit = defineEmits<{ lookup: [payload: { raw: string; assetCode: string }] }>();
const raw = ref('');
const error = ref('');

function submit() {
  const assetCode = getAssetCodeFromScan(raw.value);
  if (!assetCode) { error.value = '无法识别资产编码，请粘贴完整二维码内容或直接输入资产编码'; return; }
  error.value = '';
  emit('lookup', { raw: raw.value.trim(), assetCode });
}

</script>

<template>
  <div class="scan-card">
    <div class="scan-icon" aria-hidden="true">⌕</div><span class="eyebrow">移动扫码</span><h2>扫描资产二维码</h2>
    <p>继续使用现有二维码，系统会从原文中识别资产编码。</p>
    <form @submit.prevent="submit">
      <label class="field scan-field"><span>二维码原文或资产编码</span><input v-model="raw" placeholder="例如：CSJ-0431" autocomplete="off" autocapitalize="characters" /></label>
      <button class="primary-button full-button" type="submit">查询资产</button>
    </form>
    <QrScanner @scan="(value) => { raw = value; submit(); }" />
    <div v-if="error" class="scan-error" role="alert">{{ error }}</div>
    <div class="scan-tip">扫码结果只在本系统内查询，不会跳转到二维码中的网页。</div>
  </div>
</template>
