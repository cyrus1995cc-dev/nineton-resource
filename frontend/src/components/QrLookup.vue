<script setup lang="ts">
import { ref } from 'vue';
import QrScanner from './QrScanner.vue';
import { getAssetCodeFromScan } from '../qrLookup';
import AppIcon from './AppIcon.vue';

defineProps<{ busy?: boolean; lookupError?: string }>();

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
    <div class="scan-icon" aria-hidden="true"><AppIcon name="scan" :size="36" /></div><span class="eyebrow">移动扫码</span><h2>扫描资产二维码</h2>
    <p>旧公贝标签可以继续用。请从九吨资产内扫码，识别后查看资产详情、发起操作。</p>
    <form @submit.prevent="submit">
      <label class="field scan-field"><span>二维码原文或资产编码</span><input v-model="raw" placeholder="例如：CSJ-0431" autocomplete="off" autocapitalize="characters" /></label>
      <button class="primary-button full-button" type="submit" :disabled="busy">{{ busy ? '正在查询…' : '查询资产' }}</button>
    </form>
    <QrScanner :disabled="busy" @scan="(value) => { raw = value; submit(); }" />
    <div v-if="error || lookupError" class="scan-error" role="alert">{{ error || lookupError }}</div>
    <div class="scan-tip">在这里扫码只查询九吨资产，不打开二维码中的网页。直接用钉钉首页扫一扫旧标签，仍可能打开公贝；无法识别时可输入标签上的资产编码。</div>
  </div>
</template>
