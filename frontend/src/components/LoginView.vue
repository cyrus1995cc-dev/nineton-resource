<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { api } from '../api';
import type { AuthUser } from '../types';
import BrandLogo from './BrandLogo.vue';
import AppIcon from './AppIcon.vue';
import { isDingTalkClient, requestDingTalkAuthCode } from '../dingtalk';

const props = withDefaults(defineProps<{ autoDingTalkLogin?: boolean }>(), { autoDingTalkLogin: true });
const emit = defineEmits<{ loggedIn: [user: AuthUser] }>();
const username = ref('admin');
const password = ref('');
const busy = ref(false);
const error = ref('');
const dingtalkEnabled = ref(false);
const dingtalkCorpId = ref('');
const dingtalkClientId = ref('');
const dingtalkBusy = ref(false);
const ssoEnabled = ref(false);
const ssoBusy = ref(false);
let disposed = false;

onMounted(async () => {
  const url = new URL(window.location.href);
  const failure = url.searchParams.get('sso_error');
  if (failure) {
    error.value = failure === 'cancelled' ? '统一登录已取消或需要重新授权，请点击按钮重试。' : failure === 'state' ? '本次登录已过期或已使用，请重新登录。' : failure === 'unavailable' ? '统一登录暂时不可用，请稍后重试。' : failure === 'forbidden' ? '此账号已停用或人员授权范围待核对，请联系管理员。' : '统一登录身份验证失败，请重新登录或联系管理员。';
    url.searchParams.delete('sso_error');
    window.history.replaceState(null, '', url);
  }
  const silent = url.searchParams.get('silentSignin') === '1';
  if (silent) { url.searchParams.delete('silentSignin'); window.history.replaceState(null, '', url); }
  try {
    const sso = await api.ssoConfig();
    if (disposed) return;
    ssoEnabled.value = sso.enabled;
    if (sso.enabled && silent && !failure && props.autoDingTalkLogin) { loginWithSSO(true); return; }
  } catch { /* Keep local maintenance login accessible during SSO outages. */ }
  try {
    const config = await api.dingtalkConfig();
    if (disposed) return;
    dingtalkEnabled.value = config.enabled; dingtalkCorpId.value = config.corpId; dingtalkClientId.value = config.clientId;
    if (!ssoEnabled.value && !failure && props.autoDingTalkLogin && config.enabled && isDingTalkClient()) await loginWithDingTalk();
  } catch { /* Local maintenance login remains available if configuration cannot be fetched. */ }
});
onBeforeUnmount(() => { disposed = true; });

function loginWithSSO(silent = false) {
  if (!ssoEnabled.value || busy.value || dingtalkBusy.value || ssoBusy.value) return;
  ssoBusy.value = true;
  localStorage.removeItem('asset_token');
  const flow = new URLSearchParams(window.location.search).get('flow');
  const params = new URLSearchParams({ return: flow && /^\d{1,15}$/.test(flow) ? `/?flow=${flow}` : '/' });
  if (silent) params.set('silent', '1');
  window.location.assign(`/auth/login?${params}`);
}

async function submit() {
  if (busy.value || dingtalkBusy.value || !username.value.trim() || !password.value) return;
  busy.value = true;
  error.value = '';
  try { emit('loggedIn', await api.login(username.value.trim(), password.value)); }
  catch (err) { error.value = err instanceof Error ? err.message : '登录失败'; }
  finally { busy.value = false; }
}

async function loginWithDingTalk() {
  if (busy.value || dingtalkBusy.value) return;
  if (!dingtalkEnabled.value) { error.value = '钉钉登录尚未配置，请联系管理员'; return; }
  dingtalkBusy.value = true;
  error.value = '';
  try {
    const code = await requestDingTalkAuthCode({ corpId: dingtalkCorpId.value, clientId: dingtalkClientId.value });
    if (disposed) return;
    const user = await api.loginDingTalk(code);
    if (!disposed) emit('loggedIn', user);
  } catch (err) { if (!disposed) error.value = err instanceof Error ? err.message : '钉钉登录失败'; }
  finally { dingtalkBusy.value = false; }
}
</script>

<template>
  <main class="login-shell">
    <div class="login-layout">
    <section class="login-story" aria-label="九吨资产介绍">
      <BrandLogo />
      <div class="login-story-heading"><span class="eyebrow">每一项资产，都心中有数</span><h2>管理有条理，<br />工作更轻松。</h2><p>从入库到领用，从流转到盘点，<br />把资产的每一步，都安排妥当。</p></div>
      <div class="login-mascot"><img src="/brand/jiudun-mascot.png" alt="九吨资产吉祥物" width="360" height="360" fetchpriority="high" /></div>
      <div class="login-feature-list"><span><AppIcon name="list" />资产台账</span><span><AppIcon name="flow" />有序流转</span><span><AppIcon name="check" />轻松盘点</span></div>
    </section>
    <section class="login-card" aria-labelledby="login-title">
      <span class="login-welcome">欢迎回来</span>
      <h1 id="login-title">登录九吨资产</h1>
      <p class="login-intro">进入工作台，开始今天的资产管理。</p>
      <div v-if="error" class="error-banner login-error" role="alert">{{ error }}</div>
      <button v-if="ssoEnabled" class="primary-button full-button" :disabled="busy || dingtalkBusy || ssoBusy" @click="loginWithSSO()">{{ ssoBusy ? '正在前往统一登录…' : '使用九吨统一登录' }}</button>
      <div v-if="ssoEnabled" class="login-divider"><span>本地维护账号</span></div>
      <form class="login-form" @submit.prevent="submit">
        <label class="field"><span>用户名</span><input v-model="username" autocomplete="username" required /></label>
        <label class="field"><span>密码</span><input v-model="password" type="password" autocomplete="current-password" required /></label>
        <button class="primary-button full-button" type="submit" :disabled="busy || dingtalkBusy">{{ busy ? '登录中…' : '登录' }}</button>
      </form>
      <template v-if="!ssoEnabled"><div class="login-divider"><span>或</span></div><button class="dingtalk-button full-button" :disabled="busy || dingtalkBusy" @click="loginWithDingTalk">{{ dingtalkBusy ? '正在验证钉钉身份…' : dingtalkEnabled ? '使用钉钉登录' : '钉钉登录（未配置）' }}</button></template>
      <small class="login-hint">{{ ssoEnabled ? '首次统一登录后，请联系管理员开通资产系统权限。' : '账号由管理员分配，如需帮助请联系管理员。' }}</small>
    </section>
    </div>
    <p class="login-footer">九吨资产 · 让管理更有序</p>
  </main>
</template>
