<script setup lang="ts">
defineProps<{
  label: string
  value: string | number
  accent?: string
  subtext?: string
}>()
</script>

<template>
  <article class="metric-card panel-card" :style="{ '--accent-color': accent || 'var(--primary)' }">
    <div class="metric-header">
      <p class="metric-label">{{ label }}</p>
      <div class="metric-indicator-wrap">
        <span class="metric-indicator" />
      </div>
    </div>
    <div class="metric-body">
      <h3 class="metric-value">{{ value }}</h3>
      <p v-if="subtext" class="metric-subtext">{{ subtext }}</p>
    </div>
  </article>
</template>

<style scoped>
.metric-card {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 22px 24px;
  background: var(--surface-strong);
  border-radius: var(--radius-lg);
  border: 1px solid var(--line);
  box-shadow: 0 4px 16px rgba(45, 35, 20, 0.04);
  transition: transform 0.2s cubic-bezier(0.4, 0, 0.2, 1), box-shadow 0.2s ease;
}

.metric-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 24px rgba(45, 35, 20, 0.08);
}

.metric-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.metric-indicator-wrap {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
}

.metric-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent-color);
}

.metric-label {
  margin: 0;
  color: var(--text-secondary);
  font-size: 0.9rem;
  font-weight: 500;
}

.metric-body {
  margin-top: 14px;
}

.metric-value {
  margin: 0;
  font-size: clamp(2rem, 3.2vw, 2.4rem);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1.1;
  color: var(--text);
  letter-spacing: -0.03em;
}

.metric-subtext {
  margin: 8px 0 0;
  font-size: 0.8rem;
  color: var(--muted);
}
</style>
