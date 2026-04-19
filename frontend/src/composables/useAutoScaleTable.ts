import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

export function useAutoScaleTable() {
  const containerRef = ref<HTMLElement | null>(null)
  const tableRef = ref<HTMLElement | null>(null)
  const scale = ref(1)
  const scaledHeight = ref<number | null>(null)

  let resizeObserver: ResizeObserver | null = null

  const syncScale = () => {
    const container = containerRef.value
    const table = tableRef.value

    if (!container || !table) return

    const containerWidth = container.clientWidth
    const naturalWidth = table.scrollWidth
    const naturalHeight = table.scrollHeight
    const shouldScale = window.innerWidth <= 768 && naturalWidth > containerWidth
    const nextScale = shouldScale ? containerWidth / naturalWidth : 1

    scale.value = Math.min(1, nextScale)
    scaledHeight.value = scale.value < 1 ? naturalHeight * scale.value : null
  }

  const shellStyle = computed(() =>
    scaledHeight.value ? { height: `${scaledHeight.value}px` } : {},
  )

  const tableStyle = computed(() =>
    scale.value < 1
      ? {
          transform: `scale(${scale.value})`,
          transformOrigin: 'top left',
        }
      : {},
  )

  onMounted(async () => {
    await nextTick()
    syncScale()

    resizeObserver = new ResizeObserver(() => {
      syncScale()
    })

    if (containerRef.value) resizeObserver.observe(containerRef.value)
    if (tableRef.value) resizeObserver.observe(tableRef.value)

    window.addEventListener('resize', syncScale)
  })

  onBeforeUnmount(() => {
    resizeObserver?.disconnect()
    window.removeEventListener('resize', syncScale)
  })

  return {
    containerRef,
    tableRef,
    scale,
    shellStyle,
    tableStyle,
  }
}
