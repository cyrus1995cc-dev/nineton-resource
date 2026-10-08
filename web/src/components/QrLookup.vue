<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref } from 'vue';
import { BrowserMultiFormatReader, type IScannerControls } from '@zxing/browser';
import { getAssetCodeFromScan } from '../qrLookup';

const emit = defineEmits<{ lookup: [payload: { raw: string; assetCode: string }] }>();
const raw = ref('');
const error = ref('');
const cameraActive = ref(false);
const video = ref<HTMLVideoElement | null>(null);
let scanner: BrowserMultiFormatReader | null = null;
let controls: IScannerControls | null = null;

function submit() {
  const assetCode = getAssetCodeFromScan(raw.value);
  if (!assetCode) { error.value = '无法识别资产编码，请粘贴完整二维码内容或直接输入资产编码'; return; }
  error.value = '';
  emit('lookup', { raw: raw.value.trim(), assetCode });
}

async function stopCamera() {
  controls?.stop();
  controls = null;
  scanner?.reset();
  scanner = null;
  const stream = video.value?.srcObject as MediaStream | null;
  stream?.getTracks().forEach((track) => track.stop());
  if (video.value) video.value.srcObject = null;
  cameraActive.value = false;
}

async function openCamera() {
  if (!navigator.mediaDevices?.getUserMedia) {
    error.value = '当前环境不支持摄像头，请粘贴二维码原文或输入资产编码';
    return;
  }
  error.value = '';
  cameraActive.value = true;
  await nextTick();
  if (!video.value) return;
  try {
    scanner = new BrowserMultiFormatReader();
    controls = await scanner.decodeFromConstraints({ video: { facingMode: { ideal: 'environment' } } }, video.value, (result) => {
      if (!result) return;
      raw.value = result.getText();
      void stopCamera().then(submit);
    });
  } catch (err) {
    await stopCamera();
    error.value = err instanceof Error && /permission|denied/i.test(err.message) ? '摄像头权限未开启，请在系统设置中允许本应用使用摄像头' : '摄像头启动失败，请改用粘贴输入';
  }
}

onBeforeUnmount(() => { void stopCamera(); });
</script>

<template>
  <div class="scan-card">
    <div class="scan-icon">⌕</div><span class="eyebrow">移动扫码</span><h2>扫描资产二维码</h2>
    <p>二维码内容会在本系统内解析为资产编码，不会打开公贝云网页。</p>
    <input v-model="raw" class="scan-input" placeholder="粘贴二维码原文或输入资产编码" @keyup.enter="submit" />
    <video v-if="cameraActive" ref="video" class="scan-video" autoplay muted playsinline></video>
    <button v-if="cameraActive" class="secondary-button full-button" @click="stopCamera">关闭摄像头</button>
    <button v-else class="primary-button full-button" @click="openCamera">打开摄像头扫码</button>
    <button class="secondary-button full-button" @click="submit">查询资产</button>
    <div v-if="error" class="scan-error">{{ error }}</div><div v-else class="scan-tip">Android App 接入后将启用原生摄像头。</div>
  </div>
</template>
