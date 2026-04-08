<script setup lang="ts">
defineProps<{
  label: string
  value: string | number
  hint?: string
  accent?: string
}>()
</script>

<template>
  <article class="metric-card glass-card" :style="{ '--accent-color': accent || 'var(--primary)' }">
    <span class="metric-mark" />
    <p class="metric-label">{{ label }}</p>
    <h3 class="metric-value">{{ value }}</h3>
    <p v-if="hint" class="metric-hint">{{ hint }}</p>
  </article>
</template>

<style scoped>
.metric-card {
  position: relative;
  padding: 24px;
  overflow: hidden;
}

.metric-mark {
  position: absolute;
  inset: 16px 16px auto auto;
  width: 52px;
  height: 52px;
  border-radius: 18px;
  background: linear-gradient(135deg, var(--accent-color), rgba(255, 255, 255, 0.35));
  opacity: 0.2;
}

.metric-label {
  margin: 0;
  color: var(--muted);
  font-size: 0.95rem;
}

.metric-value {
  margin: 10px 0 8px;
  font-size: clamp(2rem, 4vw, 2.8rem);
  line-height: 1;
  letter-spacing: -0.05em;
}

.metric-hint {
  margin: 0;
  color: var(--muted);
}
</style>
