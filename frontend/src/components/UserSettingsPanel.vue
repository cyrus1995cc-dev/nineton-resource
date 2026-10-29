<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { roleLabels } from '../directory';
import { api } from '../api';
import type { ManagedUser } from '../types';

const props = defineProps<{ users: ManagedUser[]; canWrite: boolean; busy?: boolean; created?: number; currentId?: number }>();
const emit = defineEmits<{ create: [payload: { username: string; password: string; display_name: string; role: ManagedUser['role'] }]; reload: [] }>();
const ssoSource = ref(0); const ssoTarget = ref(0); const bindingBusy = ref(false);
const pendingSSO = computed(() => props.users.filter(u => u.active && u.role === 'guest' && u.sso_bound));
const bindingTargets = computed(() => props.users.filter(u => u.active && !u.sso_bound && u.role !== 'guest'));
async function bindSSO() {
  if (!ssoSource.value || !ssoTarget.value || bindingBusy.value) return;
  const source = pendingSSO.value.find(u => u.id === ssoSource.value);
  const target = bindingTargets.value.find(u => u.id === ssoTarget.value);
  if (!source || !target || !window.confirm(`确认已核实统一登录身份“${source.sso_identity?.subject}”，并将“${source.display_name}”关联到“${target.display_name}（${target.username}）”？该身份将获得已有账号的角色和业务权限，两个账号的现有登录会话都会失效。`)) return;
  bindingBusy.value = true; accountError.value = ''; accountMessage.value = '';
  try {
    await api.bindSSOUser(target.id, source.id);
    accountMessage.value = '统一登录身份已关联，请使用统一登录重新进入。';
    ssoSource.value = 0; ssoTarget.value = 0; emit('reload');
    if (target.id === props.currentId) window.location.replace('/login');
  } catch (err) { accountError.value = err instanceof Error ? err.message : '关联失败'; }
  finally { bindingBusy.value = false; }
}
const userSearch = ref(''); const userPage = ref(1);
const matchingUsers = computed(() => props.users.filter((u) => `${u.username} ${u.display_name}`.includes(userSearch.value.trim())));
const shownUsers = computed(() => matchingUsers.value.slice((userPage.value - 1) * 20, userPage.value * 20));
const accountChanges = reactive<Record<number, { role: ManagedUser['role']; active: boolean }>>({});
const accountBusy = ref(0); const accountError = ref(''); const accountMessage = ref('');
watch(() => props.users, (users) => { for (const u of users) accountChanges[u.id] = { role: u.role, active: u.active }; }, { immediate: true });
async function saveUser(user: ManagedUser) { accountBusy.value = user.id; accountError.value = ''; accountMessage.value = ''; try { await api.updateUser(user.id, accountChanges[user.id]); user.role = accountChanges[user.id].role; user.active = accountChanges[user.id].active; accountMessage.value = `${user.display_name}的权限已保存，旧会话已失效`; } catch (err) { accountError.value = err instanceof Error ? err.message : '保存失败'; } finally { accountBusy.value = 0; } }
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
    <article v-if="canWrite && pendingSSO.length" class="panel"><div class="panel-header"><div><span class="eyebrow">统一登录</span><h2>关联已有账号</h2></div></div><p>请对照统一登录平台核实人员身份编号，再将待授权的统一登录账号关联到已有账号，保留原有角色、员工关系和业务记录。</p><form class="user-form" @submit.prevent="bindSSO"><label class="field"><span>待授权的统一登录账号</span><select v-model="ssoSource" required><option :value="0">请选择</option><option v-for="u in pendingSSO" :key="u.id" :value="u.id">{{ u.display_name }} · {{ u.sso_identity?.subject }} · #{{ u.id }}</option></select></label><label class="field"><span>已有系统账号</span><select v-model="ssoTarget" required><option :value="0">请选择</option><option v-for="u in bindingTargets" :key="u.id" :value="u.id">{{ u.display_name }}（{{ u.username }}）· {{ roleLabels[u.role] }}</option></select></label><button class="primary-button full-button" type="submit" :disabled="!ssoSource || !ssoTarget || bindingBusy">{{ bindingBusy ? '关联中…' : '核对并关联' }}</button></form></article>
    <article class="panel user-list-panel"><div class="panel-header"><div><span class="eyebrow">账号和权限</span><h2>系统用户</h2></div><span class="panel-count">{{ users.length }} 个账号</span></div><p v-if="accountError" class="scan-error" role="alert">{{ accountError }}</p><p v-if="accountMessage" class="directory-message" role="status">{{ accountMessage }}</p><label class="field user-search"><span>搜索系统账号</span><input v-model="userSearch" @input="userPage = 1" /></label><div class="user-list"><div v-for="user in shownUsers" :key="user.id" class="user-row"><span class="user-avatar">{{ user.display_name.slice(0, 1) }}</span><div><strong>{{ user.display_name }}</strong><small>{{ user.username }}</small></div><div v-if="canWrite && accountChanges[user.id]" class="user-permissions"><label class="field"><span>角色</span><select v-model="accountChanges[user.id].role" :disabled="user.id === currentId || !!accountBusy" :aria-label="`${user.display_name}的角色`"><option v-for="(label, key) in roleLabels" :key="key" :value="key">{{ label }}</option></select></label><label class="user-active"><input v-model="accountChanges[user.id].active" type="checkbox" :disabled="user.id === currentId || !!accountBusy" />启用</label><button class="secondary-button" :disabled="user.id === currentId || !!accountBusy" @click="saveUser(user)">保存</button></div><span v-else class="role-pill">{{ roleLabels[user.role] }}</span></div><div v-if="!shownUsers.length" class="empty-history">暂无匹配用户</div></div><div class="pager"><span>共 {{ matchingUsers.length }} 个账号</span><div><button :disabled="userPage <= 1" @click="userPage--">上一页</button><span>{{ userPage }} / {{ Math.max(1, Math.ceil(matchingUsers.length / 20)) }}</span><button :disabled="userPage * 20 >= matchingUsers.length" @click="userPage++">下一页</button></div></div></article>
    <article class="panel user-create-panel"><div class="panel-header"><div><span class="eyebrow">管理员操作</span><h2>新增用户</h2></div></div><div v-if="!canWrite" class="empty-history">只有管理员可以管理用户</div><form v-else class="user-form" @submit.prevent="submit"><label class="field"><span>用户名</span><input v-model="username" :disabled="busy" required /></label><label class="field"><span>显示名称</span><input v-model="displayName" :disabled="busy" /></label><label class="field"><span>初始密码</span><input v-model="password" :disabled="busy" type="password" minlength="6" maxlength="200" autocomplete="new-password" required /><small>至少 6 位，仅用于本地开发或维护账号。</small></label><label class="field"><span>角色</span><select v-model="role" :disabled="busy"><option value="employee">普通员工</option><option value="approver">审批人</option><option value="viewer">只读用户</option><option value="asset_manager">资产管理员</option><option value="admin">管理员</option></select></label><button class="primary-button full-button" :disabled="busy" type="submit">{{ busy ? '保存中…' : '创建用户' }}</button></form></article>
  </div>
</template>
