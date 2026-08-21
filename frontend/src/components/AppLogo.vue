<script setup lang="ts">
withDefaults(
  defineProps<{
    size?: number
    rounded?: number
  }>(),
  {
    size: 40,
    rounded: 10,
  },
)
</script>

<template>
  <div
    class="app-logo-wrap"
    :style="{
      width: `${size}px`,
      height: `${size}px`,
      borderRadius: `${rounded}px`,
    }"
  >
    <svg
      :width="size"
      :height="size"
      viewBox="0 0 48 48"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      class="app-logo-svg"
    >
      <defs>
        <linearGradient id="dmsLogoBg" x1="0" y1="0" x2="48" y2="48" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stop-color="#1e40af" />
          <stop offset="60%" stop-color="#2563eb" />
          <stop offset="100%" stop-color="#38bdf8" />
        </linearGradient>
        <linearGradient id="dmsLogoGlow" x1="12" y1="8" x2="36" y2="40" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stop-color="#ffffff" stop-opacity="0.95" />
          <stop offset="100%" stop-color="#bae6fd" stop-opacity="0.75" />
        </linearGradient>
        <filter id="dmsShadow" x="0" y="0" width="48" height="48" filterUnits="userSpaceOnUse">
          <feDropShadow dx="0" dy="2" stdDeviation="3" flood-color="#1e3a8a" flood-opacity="0.25" />
        </filter>
      </defs>

      <!-- 外层底板 -->
      <rect width="48" height="48" :rx="rounded" fill="url(#dmsLogoBg)" />

      <!-- 网格机房与协同脉络标识 (现代科技与值班调度) -->
      <g filter="url(#dmsShadow)">
        <!-- 左机柜柱 -->
        <rect x="11" y="13" width="7" height="22" rx="2.5" fill="url(#dmsLogoGlow)" />
        <circle cx="14.5" cy="17" r="1.2" fill="#2563eb" />
        <circle cx="14.5" cy="24" r="1.2" fill="#2563eb" />
        <circle cx="14.5" cy="31" r="1.2" fill="#2563eb" />

        <!-- 中间连接脉络与调度中心 -->
        <rect x="21" y="9" width="6" height="30" rx="2.5" fill="#ffffff" />
        <rect x="23" y="14" width="2" height="4" rx="1" fill="#0284c7" />
        <rect x="23" y="22" width="2" height="4" rx="1" fill="#0284c7" />
        <rect x="23" y="30" width="2" height="4" rx="1" fill="#0284c7" />

        <!-- 右机柜柱 -->
        <rect x="30" y="13" width="7" height="22" rx="2.5" fill="url(#dmsLogoGlow)" />
        <circle cx="33.5" cy="17" r="1.2" fill="#2563eb" />
        <circle cx="33.5" cy="24" r="1.2" fill="#2563eb" />
        <circle cx="33.5" cy="31" r="1.2" fill="#2563eb" />

        <!-- 顶部桥接微光线 -->
        <path d="M14.5 13C14.5 11 20 10 24 10C28 10 33.5 11 33.5 13" stroke="#e0f2fe" stroke-width="1.5" stroke-linecap="round" stroke-opacity="0.8" />
      </g>
    </svg>
  </div>
</template>

<style scoped>
.app-logo-wrap {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 4px 14px rgba(37, 99, 235, 0.28);
  flex-shrink: 0;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}

.app-logo-wrap:hover {
  transform: translateY(-1px);
  box-shadow: 0 6px 18px rgba(37, 99, 235, 0.38);
}

.app-logo-svg {
  display: block;
  width: 100%;
  height: 100%;
}
</style>
