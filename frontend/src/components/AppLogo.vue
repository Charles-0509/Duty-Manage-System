<script setup lang="ts">
withDefaults(
  defineProps<{
    size?: number
  }>(),
  {
    size: 40,
  },
)
</script>

<template>
  <div
    class="app-logo-wrap"
    :style="{
      width: `${size}px`,
      height: `${size}px`,
    }"
  >
    <img
      src="/favicon.svg"
      alt="DMS Logo"
      class="app-logo-img"
      :style="{
        width: `${size}px`,
        height: `${size}px`,
      }"
    />
  </div>
</template>

<style scoped>
.app-logo-wrap {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: transform 0.2s ease, filter 0.2s ease;
}

.app-logo-wrap:hover {
  transform: scale(1.05);
  filter: drop-shadow(0 4px 12px rgba(134, 59, 255, 0.35));
}

.app-logo-img {
  display: block;
  object-fit: contain;
  filter: drop-shadow(0 2px 8px rgba(0, 0, 0, 0.15));
}
</style>
