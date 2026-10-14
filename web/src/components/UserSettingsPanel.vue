<script setup lang="ts">
import { ref, watch } from 'vue';
import type { ManagedUser } from '../types';

const props = defineProps<{ users: ManagedUser[]; canWrite: boolean; busy?: boolean; created?: number }>();
const emit = defineEmits<{ create: [payload: { username: string; password: string; display_name: string; role: ManagedUser['role'] }] }>();
const username = ref('');
const displayName = ref('');
const password = ref('');
const role = ref<ManagedUser['role']>('viewer');

function submit() {
  if (props.busy || !props.canWrite || !username.value.trim() || !password.value) return;
  emit('create', { username: username.value.trim(), display_name: displayName.value.trim(), password: password.value, role: role.value });
}
watch(() => props.created, () => { username.value = ''; displayName.value = ''; password.value = ''; role.value = 'viewer'; });
</script>

<template>
  <div class="settings-grid">
    <article class="panel user-list-panel"><div class="panel-header"><div><span class="eyebrow">账号和权限</span><h2>系统用户</h2></div><span class="panel-count">{{ users.length }} 个账号</span></div><div class="user-list"><div v-for="user in users" :key="user.id" class="user-row"><span class="user-avatar">{{ user.display_name.slice(0, 1) }}</span><div><strong>{{ user.display_name }}</strong><small>{{ user.username }}</small></div><span class="role-pill">{{ user.role === 'admin' ? '管理员' : user.role === 'asset_manager' ? '资产管理员' : '只读用户' }}</span></div><div v-if="!users.length" class="empty-history">暂无用户</div></div></article>
    <article class="panel user-create-panel"><div class="panel-header"><div><span class="eyebrow">管理员操作</span><h2>新增用户</h2></div></div><div v-if="!canWrite" class="empty-history">只有管理员可以管理用户</div><form v-else class="user-form" @submit.prevent="submit"><label class="field"><span>用户名</span><input v-model="username" :disabled="busy" required /></label><label class="field"><span>显示名称</span><input v-model="displayName" :disabled="busy" /></label><label class="field"><span>初始密码</span><input v-model="password" :disabled="busy" type="password" required /></label><label class="field"><span>角色</span><select v-model="role" :disabled="busy"><option value="viewer">只读用户</option><option value="asset_manager">资产管理员</option><option value="admin">管理员</option></select></label><button class="primary-button full-button" :disabled="busy" type="submit">{{ busy ? '保存中…' : '创建用户' }}</button></form></article>
  </div>
</template>
