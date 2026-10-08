<script setup lang="ts">
import { ref } from 'vue';
import { getAssetCodeFromScan } from '../qrLookup';

const emit = defineEmits<{ lookup: [payload: { raw: string; assetCode: string }] }>();
const raw = ref('');
const error = ref('');
const cameraActive = ref(false);

function submit() {
  const assetCode = getAssetCodeFromScan(raw.value);
  if (!assetCode) { error.value = '无法识别资产编码，请粘贴完整二维码内容或直接输入资产编码'; return; }
  error.value = '';
  emit('lookup', { raw: raw.value.trim(), assetCode });
}

async function openCamera() {
  const Detector = (globalThis as any).BarcodeDetector;
  if (!Detector || !navigator.mediaDevices?.getUserMedia) {
    error.value = '当前浏览器暂不支持摄像头扫码，请使用 Android App 或粘贴二维码内容';
    return;
  }
  cameraActive.value = true;
  error.value = '摄像头扫码将在 Android App 中启用，当前请使用粘贴输入';
  window.setTimeout(() => { cameraActive.value = false; }, 1800);
}
</script>

<template>
  <div class="scan-card">
    <div class="scan-icon">⌕</div><span class="eyebrow">移动扫码</span><h2>扫描资产二维码</h2>
    <p>二维码内容会在本系统内解析为资产编码，不会打开公贝云网页。</p>
    <input v-model="raw" class="scan-input" placeholder="粘贴二维码原文或输入资产编码" @keyup.enter="submit" />
    <button class="primary-button full-button" :disabled="cameraActive" @click="openCamera">{{ cameraActive ? '准备摄像头…' : '打开摄像头扫码' }}</button>
    <button class="secondary-button full-button" @click="submit">查询资产</button>
    <div v-if="error" class="scan-error">{{ error }}</div><div v-else class="scan-tip">Android App 接入后将启用原生摄像头。</div>
  </div>
</template>
