<script setup lang="ts">
defineProps<{ label: string; value: string | number; tone?: 'blue' | 'green' | 'orange' | 'purple'; hint?: string }>();
</script>

<template>
  <article class="metric-card">
    <div class="metric-top"><span class="metric-dot" :class="`tone-${tone ?? 'blue'}`"></span><span>{{ label }}</span></div>
    <strong>{{ value }}</strong>
    <small v-if="hint">{{ hint }}</small>
  </article>
</template>
