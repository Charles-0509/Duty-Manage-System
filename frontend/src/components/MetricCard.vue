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
      <span class="metric-indicator" />
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
  padding: 18px 20px;
  background: #ffffff;
  border-radius: var(--radius-lg);
  border: 1px solid var(--line);
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}

.metric-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--shadow);
}

.metric-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.metric-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent-color);
  box-shadow: 0 0 0 3px rgba(13, 148, 136, 0.15);
}

.metric-label {
  margin: 0;
  color: var(--muted);
  font-size: 0.88rem;
  font-weight: 500;
}

.metric-body {
  margin-top: 12px;
}

.metric-value {
  margin: 0;
  font-size: clamp(1.8rem, 3vw, 2.2rem);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1.1;
  color: #0f172a;
}

.metric-subtext {
  margin: 6px 0 0;
  font-size: 0.78rem;
  color: var(--muted);
}
</style>
