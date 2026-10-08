<script setup lang="ts">
import type { Asset } from '../types';

defineProps<{ items: Asset[]; total: number; page: number; pageSize: number; loading?: boolean }>();
const emit = defineEmits<{ select: [asset: Asset]; page: [page: number] }>();
</script>

<template>
  <div class="table-wrap">
    <table>
      <thead><tr><th>资产编码</th><th>资产名称</th><th>资产分类</th><th>状态</th><th>所在位置</th><th>使用人</th><th>使用部门</th><th>型号</th></tr></thead>
      <tbody>
        <tr v-if="loading"><td colspan="8" class="empty-cell">加载中…</td></tr>
        <tr v-else-if="!items.length"><td colspan="8" class="empty-cell">暂无资产数据</td></tr>
        <tr v-for="asset in items" v-else :key="asset.asset_code" @click="emit('select', asset)">
          <td><button class="link-button" @click.stop="emit('select', asset)">{{ asset.asset_code }}</button></td>
          <td>{{ asset.name || '—' }}</td><td>{{ asset.category_path || '—' }}</td>
          <td><span class="status-pill" :class="`status-${asset.status}`">{{ asset.status }}</span></td>
          <td>{{ asset.location || '—' }}</td><td>{{ asset.assignee || '—' }}</td>
          <td class="truncate">{{ asset.department || '—' }}</td><td>{{ asset.model || '—' }}</td>
        </tr>
      </tbody>
    </table>
    <div class="table-footer"><span>共 {{ total }} 条</span><div class="pager"><button :disabled="page <= 1" @click="emit('page', page - 1)">上一页</button><span>第 {{ page }} 页</span><button :disabled="page * pageSize >= total" @click="emit('page', page + 1)">下一页</button></div></div>
  </div>
</template>
