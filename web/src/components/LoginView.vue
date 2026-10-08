<script setup lang="ts">
import { ref } from 'vue';
import { api } from '../api';
import type { AuthUser } from '../types';

const emit = defineEmits<{ loggedIn: [user: AuthUser] }>();
const username = ref('admin');
const password = ref('admin123');
const busy = ref(false);
const error = ref('');
const dingtalkEnabled = ref(false);
const dingtalkCorpId = ref('');
const dingtalkBusy = ref(false);

void api.dingtalkConfig().then((config) => { dingtalkEnabled.value = config.enabled; dingtalkCorpId.value = config.corpId; }).catch(() => undefined);

async function submit() {
  if (!username.value.trim() || !password.value) return;
  busy.value = true;
  error.value = '';
  try { emit('loggedIn', await api.login(username.value.trim(), password.value)); }
  catch (err) { error.value = err instanceof Error ? err.message : '登录失败'; }
  finally { busy.value = false; }
}

async function loginWithDingTalk() {
  if (!dingtalkEnabled.value) { error.value = '钉钉登录尚未配置，请联系管理员'; return; }
  const dd = window.dd;
  if (!dd) { error.value = '请在钉钉工作台中打开此页面'; return; }
  dingtalkBusy.value = true;
  error.value = '';
  try {
    const requestAuthCode = dd.runtime?.permission?.requestAuthCode ?? dd.getAuthCode;
    if (!requestAuthCode) throw new Error('当前钉钉环境不支持免登');
    const result = await new Promise<{ code?: string; errCode?: number; errMsg?: string }>((resolve, reject) => {
      let settled = false;
      const done = (value: { code?: string; errCode?: number; errMsg?: string }) => { if (settled) return; settled = true; resolve(value); };
      try {
        const returned = requestAuthCode({ corpId: dingtalkCorpId.value }, done);
        if (returned && typeof (returned as Promise<unknown>).then === 'function') (returned as Promise<{ code?: string; errCode?: number; errMsg?: string }>).then(done).catch(reject);
      } catch (err) { reject(err); }
    });
    if (!result.code) throw new Error(result.errMsg || '未取得钉钉授权码');
    emit('loggedIn', await api.loginDingTalk(result.code));
  } catch (err) { error.value = err instanceof Error ? err.message : '钉钉登录失败'; }
  finally { dingtalkBusy.value = false; }
}
</script>

<template>
  <main class="login-shell">
    <section class="login-card">
      <div class="login-brand"><span class="brand-mark">◒</span><span>自有资产</span></div>
      <span class="eyebrow">资产管理平台</span>
      <h1>登录系统</h1>
      <p class="login-intro">使用本地账号进入资产台账和流转工作台。</p>
      <div v-if="error" class="error-banner login-error">{{ error }}</div>
      <form class="login-form" @submit.prevent="submit">
        <label class="field"><span>用户名</span><input v-model="username" autocomplete="username" required /></label>
        <label class="field"><span>密码</span><input v-model="password" type="password" autocomplete="current-password" required /></label>
        <button class="primary-button full-button" type="submit" :disabled="busy">{{ busy ? '登录中…' : '登录' }}</button>
      </form>
      <div class="login-divider"><span>或</span></div>
      <button class="dingtalk-button full-button" :disabled="dingtalkBusy" @click="loginWithDingTalk">{{ dingtalkBusy ? '钉钉登录中…' : dingtalkEnabled ? '使用钉钉登录' : '钉钉登录（未配置）' }}</button>
      <small class="login-hint">首次运行默认账号：admin / admin123</small>
    </section>
  </main>
</template>
