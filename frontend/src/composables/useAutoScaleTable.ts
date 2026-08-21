import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue"

export function useAutoScaleTable() {
  const containerRef = ref<HTMLElement | null>(null)
  const tableRef = ref<HTMLElement | null>(null)
  const scale = ref(1)
  const scaledHeight = ref<number | null>(null)
  const isScrollMode = ref(false)

  let resizeObserver: ResizeObserver | null = null
  let animationFrame = 0

  const measureScale = () => {
    const container = containerRef.value
    const table = tableRef.value

    if (!container || !table) return

    const containerWidth = container.clientWidth
    const naturalWidth = table.scrollWidth
    const naturalHeight = table.scrollHeight

    // Check if table needs horizontal scroll (i.e. mobile or narrow container)
    const needsScroll = window.innerWidth <= 768 || (naturalWidth > containerWidth && window.innerWidth <= 1024)
    isScrollMode.value = needsScroll

    const shouldScale = window.innerWidth > 768 && naturalWidth > containerWidth
    const nextScale = Math.min(1, shouldScale ? containerWidth / naturalWidth : 1)
    const nextHeight = nextScale < 1 ? naturalHeight * nextScale : null

    if (Math.abs(scale.value - nextScale) > 0.001) {
      scale.value = nextScale
    }
    if (
      (scaledHeight.value === null) !== (nextHeight === null)
      || (nextHeight !== null && Math.abs((scaledHeight.value || 0) - nextHeight) > 0.5)
    ) {
      scaledHeight.value = nextHeight
    }
  }

  const syncScale = () => {
    if (animationFrame) return
    animationFrame = window.requestAnimationFrame(() => {
      animationFrame = 0
      measureScale()
    })
  }

  const shellStyle = computed(() =>
    scaledHeight.value ? { height: String(scaledHeight.value) + "px" } : {},
  )

  const tableStyle = computed(() =>
    scale.value < 1
      ? {
          transform: "scale(" + scale.value + ")",
          transformOrigin: "top left",
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

    window.addEventListener("resize", syncScale)
  })

  onBeforeUnmount(() => {
    resizeObserver?.disconnect()
    if (animationFrame) window.cancelAnimationFrame(animationFrame)
    window.removeEventListener("resize", syncScale)
  })

  return {
    containerRef,
    tableRef,
    scale,
    isScrollMode,
    shellStyle,
    tableStyle,
  }
}
