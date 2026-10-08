<script setup lang="ts">
import { ref } from 'vue';
import { api } from '../api';
import type { AuthUser } from '../types';

const emit = defineEmits<{ loggedIn: [user: AuthUser] }>();
const username = ref('admin');
const password = ref('admin123');
const busy = ref(false);
const error = ref('');

async function submit() {
  if (!username.value.trim() || !password.value) return;
  busy.value = true;
  error.value = '';
  try { emit('loggedIn', await api.login(username.value.trim(), password.value)); }
  catch (err) { error.value = err instanceof Error ? err.message : '登录失败'; }
  finally { busy.value = false; }
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
      <small class="login-hint">首次运行默认账号：admin / admin123</small>
    </section>
  </main>
</template>
